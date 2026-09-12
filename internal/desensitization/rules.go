package desensitization

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	reEmail = regexp.MustCompile(`(?i)\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	// CN mobile with optional +86 / 86 and spaces/dashes.
	rePhoneLoose = regexp.MustCompile(`(?i)(?:\+?86[\s\-]*)?(?:1[3-9](?:[\s\-]?\d){9})`)
	reIDCard18   = regexp.MustCompile(`\b[1-9]\d{5}(?:19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]\b`)
	reIDCard15   = regexp.MustCompile(`\b[1-9]\d{5}\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}\b`)
	rePrivateKey = regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----[\s\S]{20,}?-----END[^-]*PRIVATE KEY-----`)
	reConnstr    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://[^\s:@/"']+:[^\s@/"']{1,}@[^\s"'<>]+`)
	reJWT        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	reBearer     = regexp.MustCompile(`(?i)\bBearer\s+([A-Za-z0-9._~+/=-]{20,})`)
	reGHPAT      = regexp.MustCompile(`(?i)\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{20,}\b`)
	reGHFine     = regexp.MustCompile(`(?i)\bgithub_pat_[A-Za-z0-9_]{40,}\b`)
	reAIza       = regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35,}\b`)
	reAKIA       = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	reCardLoose  = regexp.MustCompile(`\b(?:\d[ \-]?){12,18}\d\b`)
	reIPPrivate  = regexp.MustCompile(`\b(?:192\.168(?:\.\d{1,3}){2}|10(?:\.\d{1,3}){3}|172\.(?:1[6-9]|2\d|3[0-1])(?:\.\d{1,3}){2})\b`)
	reIPInternal = regexp.MustCompile(`\b(?:10(?:\.\d{1,3}){3}|172\.(?:1[6-9]|2\d|3[0-1])(?:\.\d{1,3}){2})\b`)
	reMAC        = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b`)
	rePlate      = regexp.MustCompile(`[\p{Han}][A-Z][A-HJ-NP-Z0-9]{4,5}[A-HJ-NP-Z0-9挂学警港澳]`)
	reLandline   = regexp.MustCompile(`\b0\d{2,3}[\s\-]?\d{7,8}\b`)
	// SECRET_ASSIGNMENT stays category-default off (high FP). CN keywords 口令 / 登录密码 included.
	reSecretAsgn  = regexp.MustCompile(`(?i)(?:\b(?:password|passwd|pwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret)\b|口令|登录密码)\s*[:=：]\s*([^\s"'\\]{4,})`)
	rePlaceholder = regexp.MustCompile(`\{\{[A-Z][A-Z0-9]*_[a-z0-9]{6,8}\}\}`)
	rePartialTok  = regexp.MustCompile(`\{\{[A-Za-z0-9_]{0,24}$`)
)

var idCardProvinces = map[string]struct{}{
	"11": {}, "12": {}, "13": {}, "14": {}, "15": {},
	"21": {}, "22": {}, "23": {},
	"31": {}, "32": {}, "33": {}, "34": {}, "35": {}, "36": {}, "37": {},
	"41": {}, "42": {}, "43": {}, "44": {}, "45": {}, "46": {},
	"50": {}, "51": {}, "52": {}, "53": {}, "54": {},
	"61": {}, "62": {}, "63": {}, "64": {}, "65": {},
	"71": {}, "81": {}, "82": {}, "91": {},
}

var idCardWeights = []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
var idCardChecks = []byte("10X98765432")

func looksLikePlaceholder(s string) bool {
	return rePlaceholder.MatchString(strings.TrimSpace(s))
}

func validatePhone(raw string) bool {
	digits := onlyDigits(raw)
	if strings.HasPrefix(digits, "86") && len(digits) == 13 {
		digits = digits[2:]
	}
	if len(digits) != 11 || digits[0] != '1' || digits[1] < '3' || digits[1] > '9' {
		return false
	}
	allSame := true
	for i := 1; i < len(digits); i++ {
		if digits[i] != digits[0] {
			allSame = false
			break
		}
	}
	return !allSame
}

func validateIDCard(raw string) bool {
	s := strings.ToUpper(strings.TrimSpace(raw))
	switch len(s) {
	case 15:
		if _, ok := idCardProvinces[s[:2]]; !ok {
			return false
		}
		yy, mm, dd := s[6:8], s[8:10], s[10:12]
		return plausibleDate("19"+yy, mm, dd)
	case 18:
		if _, ok := idCardProvinces[s[:2]]; !ok {
			return false
		}
		if !plausibleDate(s[6:10], s[10:12], s[12:14]) {
			return false
		}
		sum := 0
		for i := 0; i < 17; i++ {
			d := int(s[i] - '0')
			if d < 0 || d > 9 {
				return false
			}
			sum += d * idCardWeights[i]
		}
		return idCardChecks[sum%11] == s[17]
	default:
		return false
	}
}

func plausibleDate(yyyy, mm, dd string) bool {
	y, errY := strconv.Atoi(yyyy)
	m, errM := strconv.Atoi(mm)
	d, errD := strconv.Atoi(dd)
	if errY != nil || errM != nil || errD != nil {
		return false
	}
	if y < 1900 || y > time.Now().Year() {
		return false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return t.Year() == y && int(t.Month()) == m && t.Day() == d
}

func validateJWT(raw string) bool {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return false
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		// try std encoding without padding issues
		padded := parts[0]
		if m := len(padded) % 4; m != 0 {
			padded += strings.Repeat("=", 4-m)
		}
		headerJSON, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return false
		}
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return false
	}
	_, ok := header["alg"]
	return ok
}

func luhnOK(digits string) bool {
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := int(digits[i] - '0')
		if n < 0 || n > 9 {
			return false
		}
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func highEntropyAfterPrefix(s, prefix string) bool {
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	if len(rest) < 16 {
		return false
	}
	alnum := 0
	for _, r := range rest {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			alnum++
		} else if unicode.IsSpace(r) {
			break
		} else {
			return false
		}
	}
	return alnum >= 16
}

func looksLikeBase64Blob(s string) bool {
	if len(s) < 512 {
		return false
	}
	sample := s
	if len(sample) > 256 {
		sample = sample[:256]
	}
	ok := 0
	for _, r := range sample {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' || r == '\n' || r == '\r' {
			ok++
		}
	}
	return float64(ok)/float64(len(sample)) > 0.95
}
