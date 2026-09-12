package desensitization

import (
	"regexp"
	"strings"
	"unicode"
)

type matchSpan struct {
	start, end int
	category   string
	original   string
}

func (e *Engine) maskText(sessionID, text string, counts map[string]int) string {
	if text == "" || looksLikePlaceholder(text) {
		return text
	}
	if looksLikeBase64Blob(text) {
		return text
	}

	// Protect existing placeholders from nested remasking.
	protected := map[string]string{}
	text = rePlaceholder.ReplaceAllStringFunc(text, func(m string) string {
		key := "\x00PH" + randomID(6) + "\x00"
		protected[key] = m
		return key
	})

	spans := e.findSpans(text)
	if len(spans) == 0 {
		for k, v := range protected {
			text = strings.ReplaceAll(text, k, v)
		}
		return text
	}

	// Resolve overlaps: earlier / longer wins.
	spans = resolveOverlaps(spans)
	var b strings.Builder
	b.Grow(len(text) + 32)
	cursor := 0
	for _, sp := range spans {
		if sp.start < cursor {
			continue
		}
		b.WriteString(text[cursor:sp.start])
		tok := e.tokenFor(sessionID, sp.category, sp.original)
		b.WriteString(tok)
		if counts != nil {
			counts[sp.category]++
		}
		cursor = sp.end
	}
	b.WriteString(text[cursor:])
	out := b.String()
	for k, v := range protected {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

func (e *Engine) findSpans(text string) []matchSpan {
	var spans []matchSpan
	add := func(start, end int, cat, orig string) {
		if start < 0 || end > len(text) || start >= end {
			return
		}
		if !e.cfg.CategoryEnabled(cat) && cat != "TERM" && cat != "CUSTOM" {
			return
		}
		spans = append(spans, matchSpan{start: start, end: end, category: cat, original: orig})
	}

	// Custom terms first (longest first for priority).
	type termSpec struct {
		value string
		cat   string
		whole bool
	}
	var termList []termSpec
	for _, t := range e.cfg.CustomTerms {
		cat := strings.ToUpper(strings.TrimSpace(t.Category))
		if cat == "" {
			cat = "TERM"
		}
		termList = append(termList, termSpec{value: t.Value, cat: cat, whole: t.WholeWord})
	}
	// Sort by length descending (simple insertion).
	for i := 0; i < len(termList); i++ {
		for j := i + 1; j < len(termList); j++ {
			if len(termList[j].value) > len(termList[i].value) {
				termList[i], termList[j] = termList[j], termList[i]
			}
		}
	}
	for _, t := range termList {
		if t.value == "" {
			continue
		}
		search := text
		offset := 0
		for {
			idx := strings.Index(search, t.value)
			if idx < 0 {
				break
			}
			abs := offset + idx
			end := abs + len(t.value)
			if !t.whole || isWholeWord(text, abs, end) {
				add(abs, end, t.cat, text[abs:end])
			}
			search = text[end:]
			offset = end
		}
	}

	if e.cfg.CategoryEnabled("PRIVATE_KEY") {
		for _, m := range rePrivateKey.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "PRIVATE_KEY", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("CONNSTR") {
		for _, m := range reConnstr.FindAllStringIndex(text, -1) {
			// Whole-string token for the connection string (simpler + safer than password-only).
			add(m[0], m[1], "CONNSTR", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("API_KEY") {
		for _, re := range []*regexp.Regexp{reGHPAT, reGHFine, reAIza, reAKIA} {
			for _, m := range re.FindAllStringIndex(text, -1) {
				add(m[0], m[1], "API_KEY", text[m[0]:m[1]])
			}
		}
		for _, re := range e.prefixRes {
			for _, m := range re.FindAllStringIndex(text, -1) {
				if isASCIIWordBoundary(text, m[0], m[1]) {
					add(m[0], m[1], "API_KEY", text[m[0]:m[1]])
				}
			}
		}
	}
	if e.cfg.CategoryEnabled("TOKEN") {
		for _, m := range reBearer.FindAllStringSubmatchIndex(text, -1) {
			// Replace the token value group if present, else whole match.
			if len(m) >= 4 {
				add(m[2], m[3], "TOKEN", text[m[2]:m[3]])
			} else {
				add(m[0], m[1], "TOKEN", text[m[0]:m[1]])
			}
		}
	}
	if e.cfg.CategoryEnabled("ACCESS_KEY") {
		for _, m := range reAKIA.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "ACCESS_KEY", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("JWT") {
		for _, m := range reJWT.FindAllStringIndex(text, -1) {
			cand := text[m[0]:m[1]]
			if validateJWT(cand) {
				add(m[0], m[1], "JWT", cand)
			}
		}
	}
	if e.cfg.CategoryEnabled("SECRET_ASSIGNMENT") {
		for _, m := range reSecretAsgn.FindAllStringSubmatchIndex(text, -1) {
			if len(m) >= 4 {
				add(m[2], m[3], "SECRET_ASSIGNMENT", text[m[2]:m[3]])
			}
		}
	}
	if e.cfg.CategoryEnabled("EMAIL") {
		for _, m := range reEmail.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "EMAIL", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("PHONE") {
		for _, m := range rePhoneLoose.FindAllStringIndex(text, -1) {
			cand := text[m[0]:m[1]]
			if validatePhone(cand) {
				add(m[0], m[1], "PHONE", cand)
			}
		}
	}
	if e.cfg.CategoryEnabled("IDCARD") {
		for _, m := range reIDCard18.FindAllStringIndex(text, -1) {
			cand := text[m[0]:m[1]]
			if validateIDCard(cand) {
				add(m[0], m[1], "IDCARD", cand)
			}
		}
		for _, m := range reIDCard15.FindAllStringIndex(text, -1) {
			cand := text[m[0]:m[1]]
			if validateIDCard(cand) {
				add(m[0], m[1], "IDCARD", cand)
			}
		}
	}
	if e.cfg.CategoryEnabled("CARD") {
		for _, m := range reCardLoose.FindAllStringIndex(text, -1) {
			cand := text[m[0]:m[1]]
			digits := onlyDigits(cand)
			if luhnOK(digits) {
				add(m[0], m[1], "CARD", cand)
			}
		}
	}
	if e.cfg.CategoryEnabled("IP_PRIVATE") {
		for _, m := range reIPPrivate.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "IP_PRIVATE", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("IP_INTERNAL") {
		for _, m := range reIPInternal.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "IP_INTERNAL", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("MAC") {
		for _, m := range reMAC.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "MAC", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("PLATE") {
		for _, m := range rePlate.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "PLATE", text[m[0]:m[1]])
		}
	}
	if e.cfg.CategoryEnabled("LANDLINE") {
		for _, m := range reLandline.FindAllStringIndex(text, -1) {
			add(m[0], m[1], "LANDLINE", text[m[0]:m[1]])
		}
	}
	for i, re := range e.customs {
		cat := "CUSTOM"
		if i < len(e.customCat) {
			cat = e.customCat[i]
		}
		for _, m := range re.FindAllStringIndex(text, -1) {
			add(m[0], m[1], cat, text[m[0]:m[1]])
		}
	}
	return spans
}

func resolveOverlaps(spans []matchSpan) []matchSpan {
	if len(spans) <= 1 {
		return spans
	}
	// Sort by start asc, length desc.
	for i := 0; i < len(spans); i++ {
		for j := i + 1; j < len(spans); j++ {
			if spans[j].start < spans[i].start ||
				(spans[j].start == spans[i].start && (spans[j].end-spans[j].start) > (spans[i].end-spans[i].start)) {
				spans[i], spans[j] = spans[j], spans[i]
			}
		}
	}
	out := make([]matchSpan, 0, len(spans))
	lastEnd := -1
	for _, sp := range spans {
		if sp.start < lastEnd {
			continue
		}
		out = append(out, sp)
		lastEnd = sp.end
	}
	return out
}

func isWholeWord(text string, start, end int) bool {
	if start > 0 {
		r := rune(text[start-1])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r > 0x4e00 {
			return false
		}
	}
	if end < len(text) {
		r := rune(text[end])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r > 0x4e00 {
			return false
		}
	}
	return true
}

func isASCIIWordBoundary(text string, start, end int) bool {
	if start > 0 {
		c := text[start-1]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			return false
		}
	}
	if end < len(text) {
		c := text[end]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			return false
		}
	}
	return true
}
