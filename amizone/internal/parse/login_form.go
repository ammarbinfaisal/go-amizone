package parse

import (
	"io"

	"github.com/PuerkitoBio/goquery"
	"k8s.io/klog/v2"
)

// LoginFormFields contains all the fields needed for login submission
type LoginFormFields struct {
	VerificationToken string
	Salt              string
	SecretNumber      string
	Signature         string
	Challenge         string
	TurnstileSiteKey  string
	RecaptchaSiteKey  string
	// These are filled after CAPTCHA is solved
	TurnstileResponse string
	RecaptchaToken    string
	Honeypot          string
}

// ParseLoginForm extracts all hidden form fields from the login page
func ParseLoginForm(body io.Reader) (*LoginFormFields, error) {
	dom, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		klog.Errorf("failed to parse login page: %s", err.Error())
		return nil, err
	}

	return ParseLoginFormFromDom(dom), nil
}

// ParseLoginFormFromDom extracts all hidden form fields from the login page DOM
func ParseLoginFormFromDom(dom *goquery.Document) *LoginFormFields {
	form := dom.Find("form#loginform")

	fields := &LoginFormFields{
		VerificationToken: form.Find("input[name='__RequestVerificationToken']").AttrOr("value", ""),
		Salt:              form.Find("input[name='Salt']").AttrOr("value", ""),
		SecretNumber:      form.Find("input[name='SecretNumber']").AttrOr("value", ""),
		Signature:         form.Find("input[name='Signature']").AttrOr("value", ""),
		Challenge:         form.Find("input[name='Challenge']").AttrOr("value", ""),
		TurnstileResponse: form.Find("input[name='cf-turnstile-response']").AttrOr("value", ""),
		RecaptchaToken:    form.Find("input[name='RecaptchaToken']").AttrOr("value", ""),
		Honeypot:          form.Find("input[name='honeypot']").AttrOr("value", ""),
	}

	// Extract turnstile site key from script
	dom.Find("script").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		// Look for sitekey in turnstile.render call
		if fields.TurnstileSiteKey == "" && len(text) > 0 {
			// Simple extraction - in production you might want regex
			if idx := findSubstring(text, `sitekey: "`); idx >= 0 {
				start := idx + len(`sitekey: "`)
				end := findSubstring(text[start:], `"`)
				if end > 0 {
					fields.TurnstileSiteKey = text[start : start+end]
				}
			}
		}
	})

	klog.V(2).Infof("Parsed login form fields: token=%s, salt=%s, secretNum=%s, sig=%s..., challenge=%s..., siteKey=%s",
		truncate(fields.VerificationToken, 20),
		fields.Salt,
		fields.SecretNumber,
		truncate(fields.Signature, 10),
		truncate(fields.Challenge, 10),
		fields.TurnstileSiteKey,
	)

	return fields
}

// IsValid checks if the essential fields are present
func (f *LoginFormFields) IsValid() bool {
	return f.VerificationToken != "" && f.Salt != "" && f.SecretNumber != "" && f.Signature != "" && f.Challenge != ""
}

// HasTurnstileToken checks if the turnstile token has been set
func (f *LoginFormFields) HasTurnstileToken() bool {
	return f.TurnstileResponse != "" || f.RecaptchaToken != ""
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
