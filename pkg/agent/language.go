package agent

import "strings"

// DetectResponseLanguage returns a BCP-47 style language tag for reply policy.
// Priority: explicit preference > current user text > last session language > default English.
func DetectResponseLanguage(userText, preferred, last string) string {
	if p := normalizeLang(preferred); p != "" {
		return p
	}
	if detected := detectFromText(userText); detected != "" {
		return detected
	}
	if l := normalizeLang(last); l != "" {
		return l
	}
	return "en"
}

func detectFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	var zh, ja, ko, letters int
	for _, r := range text {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF:
			zh++
		case r >= 0x3040 && r <= 0x30FF:
			ja++
		case r >= 0xAC00 && r <= 0xD7AF:
			ko++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			letters++
		}
	}

	// CJK-first heuristic to match user typing language quickly.
	if zh > 0 {
		return "zh-CN"
	}
	if ja > 0 {
		return "ja"
	}
	if ko > 0 {
		return "ko"
	}
	if letters > 0 {
		return "en"
	}
	return ""
}

func normalizeLang(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	switch lang {
	case "zh", "zh-cn", "zh_hans", "chinese":
		return "zh-CN"
	case "en", "en-us", "english":
		return "en"
	case "ja", "jp", "japanese":
		return "ja"
	case "ko", "kr", "korean":
		return "ko"
	default:
		if lang == "" {
			return ""
		}
		return lang
	}
}

// ExtractLanguagePreference detects explicit user instructions for language switch.
func ExtractLanguagePreference(text string) string {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return ""
	}

	enHints := []string{"speak english", "reply in english", "use english", "以后用英文", "请用英文", "用英文"}
	zhHints := []string{"说中文", "用中文", "请用中文", "reply in chinese", "speak chinese"}
	jaHints := []string{"日本語", "reply in japanese", "speak japanese"}
	koHints := []string{"한국어", "reply in korean", "speak korean"}

	for _, h := range enHints {
		if strings.Contains(s, strings.ToLower(h)) {
			return "en"
		}
	}
	for _, h := range zhHints {
		if strings.Contains(s, strings.ToLower(h)) {
			return "zh-CN"
		}
	}
	for _, h := range jaHints {
		if strings.Contains(s, strings.ToLower(h)) {
			return "ja"
		}
	}
	for _, h := range koHints {
		if strings.Contains(s, strings.ToLower(h)) {
			return "ko"
		}
	}
	return ""
}
