package mail

import (
	"regexp"
	"strings"
)

// otpRe matches standalone 4-8 digit runs. Surrounding context is checked
// by hasOTPHint so random order numbers, years, and amounts don't match.
var otpRe = regexp.MustCompile(`(?m)(?:^|[\s:>"'(\[-])(\d{4,8})(?:[\s<.,;"')\]-]|$)`)

// hintRe scores the text around a candidate: OTP emails almost always carry
// one of these code-specific words within a short window of the code.
// Generic words like "confirmed" are excluded on purpose — order
// confirmations carry 6-digit numbers that must not match.
var hintRe = regexp.MustCompile(`(?i)(otp|one[- ]?time\s+(code|password|passcode)|verification|verif(y|ication)|passcode|\bcode\b|2fa|two[- ]?factor|authenticat|login code|sign[- ]?in code)`)

// ExtractOTP scans subject+text+html for the most likely one-time code.
// It returns the code and true when a digit run has OTP context nearby;
// otherwise "", false. HTML tags are stripped before matching.
func ExtractOTP(subject, text, html string) (string, bool) {
	body := stripTags(html)
	joined := subject + "\n" + text + "\n" + body
	locs := otpRe.FindAllStringSubmatchIndex(joined, -1)
	best := ""
	bestScore := -1
	for _, loc := range locs {
		if len(loc) < 4 {
			continue
		}
		code := joined[loc[2]:loc[3]]
		if !plausibleOTP(code) {
			continue
		}
		// 120-char window around the candidate for hint words.
		start := loc[0] - 120
		if start < 0 {
			start = 0
		}
		end := loc[1] + 120
		if end > len(joined) {
			end = len(joined)
		}
		window := joined[start:end]
		score := 0
		if hintRe.MatchString(window) {
			score += 2
		}
		// Prefer codes in the subject line (highest signal).
		if loc[0] < len(subject)+1 {
			score += 2
		}
		// Prefer 6-digit codes (most common OTP length).
		if len(code) == 6 {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = code
		}
	}
	return best, bestScore >= 2
}

// plausibleOTP rejects digit runs that are almost never OTPs: years,
// round amounts, and repeated/sequential runs.
func plausibleOTP(code string) bool {
	if len(code) < 4 || len(code) > 8 {
		return false
	}
	// Years 1900-2099.
	if len(code) == 4 && code[0] >= '1' && code[:2] >= "19" && code[:2] <= "20" {
		y := code
		if y >= "1900" && y <= "2099" {
			return false
		}
	}
	// All-same-digit runs (1111, 000000).
	same := true
	for i := 1; i < len(code); i++ {
		if code[i] != code[0] {
			same = false
			break
		}
	}
	if same {
		return false
	}
	return true
}

// stripTags removes HTML markup, leaving visible text with spacing so
// words on either side of a tag don't glue together.
func stripTags(html string) string {
	if html == "" {
		return ""
	}
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
			b.WriteRune(' ')
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
