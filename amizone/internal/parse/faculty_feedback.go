package parse

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/ditsuke/go-amizone/amizone/models"
)

func isFacultyPage(dom *goquery.Document) bool {
	const FacultyPageBreadcrumb = "My Faculty"
	return CleanString(dom.Find(selectorActiveBreadcrumb).Text()) == FacultyPageBreadcrumb
}

func FacultyFeedback(body io.Reader) (models.FacultyFeedbackSpecs, error) {
	dom, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", ErrFailedToParseDOM, err)
	}

	if !IsLoggedInDOM(dom) {
		return nil, errors.New(ErrNotLoggedIn)
	}

	if !isFacultyPage(dom) {
		return nil, fmt.Errorf("%s: Not Faculty Feedback Page", ErrFailedToParse)
	}

	specs := make(models.FacultyFeedbackSpecs, 0)
	feedbackEndpoint := inferFeedbackEndpoint(dom)
	submitEndpoint := inferFeedbackSubmitEndpoint(feedbackEndpoint)
	verificationToken := VerificationTokenFromDom(dom)
	seen := make(map[string]struct{})

	appendSpec := func(spec models.FacultyFeedbackSpec) {
		if spec.FacultyId == "" || spec.CourseType == "" || spec.DepartmentId == "" || spec.SerialNumber == "" {
			return
		}
		key := strings.Join([]string{
			spec.SubmitEndpoint,
			spec.FeedbackEndpoint,
			spec.FeedbackMethod,
			spec.FeedbackPayload,
			spec.FacultyId,
			spec.CourseType,
			spec.DepartmentId,
			spec.SerialNumber,
		}, "|")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		specs = append(specs, spec)
	}

	dom.Find(`a[href*="_FeedbackRating"]`).Each(func(_ int, anchor *goquery.Selection) {
		spec, ok := specFromFeedbackAnchor(verificationToken, submitEndpoint, anchor)
		if ok {
			appendSpec(spec)
		}
	})

	dom.Find(`[onclick], a[href]`).Each(func(_ int, sel *goquery.Selection) {
		attr := sel.AttrOr("onclick", "")
		if attr == "" {
			attr = sel.AttrOr("href", "")
		}
		if !strings.Contains(attr, "FnFeedBackAgian(") {
			return
		}
		spec, ok := specFromFeedbackCall(verificationToken, feedbackEndpoint, submitEndpoint, attr)
		if ok {
			appendSpec(spec)
		}
	})

	return specs, nil
}

func inferFeedbackEndpoint(dom *goquery.Document) string {
	const defaultFeedbackEndpoint = "/FacultyFeeback/FacultyFeedback/_FeedbackRating"

	html, err := dom.Html()
	if err != nil {
		return defaultFeedbackEndpoint
	}

	re := regexp.MustCompile(`url:\s*['"]([^'"]+/_FeedbackRating)['"]`)
	match := re.FindStringSubmatch(html)
	if len(match) < 2 {
		return defaultFeedbackEndpoint
	}

	return match[1]
}

func inferFeedbackSubmitEndpoint(feedbackEndpoint string) string {
	if feedbackEndpoint == "" {
		return "/FacultyFeeback/FacultyFeedback/SaveFeedbackRating"
	}
	return strings.TrimSuffix(feedbackEndpoint, "/_FeedbackRating") + "/SaveFeedbackRating"
}

func specFromFeedbackAnchor(verificationToken string, submitEndpoint string, anchor *goquery.Selection) (models.FacultyFeedbackSpec, bool) {
	rawURI := anchor.AttrOr("href", "")
	uri, err := url.Parse(rawURI)
	if err != nil {
		return models.FacultyFeedbackSpec{}, false
	}

	query := uri.Query()
	spec := models.FacultyFeedbackSpec{
		VerificationToken: verificationToken,
		FeedbackEndpoint:  rawURI,
		FeedbackMethod:    strings.ToUpper(firstNonEmpty(anchor.AttrOr("data-ajax-method", ""), http.MethodPost)),
		SubmitEndpoint:    submitEndpoint,
		FacultyId:         escapeFeedbackValue(firstNonEmpty(query.Get("FacultyStaffID"), query.Get("FacultyId"))),
		CourseType:        escapeFeedbackValue(firstNonEmpty(query.Get("CourseType"), query.Get("sType"))),
		DepartmentId:      escapeFeedbackValue(firstNonEmpty(query.Get("DetID"), query.Get("iDetId"))),
		SerialNumber:      escapeFeedbackValue(firstNonEmpty(query.Get("SrNo"), query.Get("iSRNO"))),
	}

	if spec.FacultyId == "" || spec.CourseType == "" || spec.DepartmentId == "" || spec.SerialNumber == "" {
		return models.FacultyFeedbackSpec{}, false
	}

	return spec, true
}

func specFromFeedbackCall(verificationToken string, feedbackEndpoint string, submitEndpoint string, rawCall string) (models.FacultyFeedbackSpec, bool) {
	start := strings.Index(rawCall, "FnFeedBackAgian(")
	if start == -1 {
		return models.FacultyFeedbackSpec{}, false
	}

	argsBlock := rawCall[start+len("FnFeedBackAgian("):]
	end := findCallEnd(argsBlock)
	if end == -1 {
		return models.FacultyFeedbackSpec{}, false
	}
	args := parseJSCallArgs(argsBlock[:end])
	if len(args) < 7 {
		return models.FacultyFeedbackSpec{}, false
	}

	spec := models.FacultyFeedbackSpec{
		VerificationToken: verificationToken,
		FeedbackEndpoint:  feedbackEndpoint,
		FeedbackMethod:    http.MethodPost,
		FeedbackPayload:   buildFeedbackCallPayload(args),
		SubmitEndpoint:    submitEndpoint,
		FacultyId:         escapeFeedbackValue(args[3]),
		SerialNumber:      escapeFeedbackValue(args[4]),
		CourseType:        escapeFeedbackValue(args[5]),
		DepartmentId:      escapeFeedbackValue(args[6]),
	}

	if spec.FacultyId == "" || spec.CourseType == "" || spec.DepartmentId == "" || spec.SerialNumber == "" {
		return models.FacultyFeedbackSpec{}, false
	}

	return spec, true
}

func parseJSCallArgs(raw string) []string {
	args := make([]string, 0, 7)
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		part := strings.TrimSpace(current.String())
		part = strings.Trim(part, `"'`)
		if part != "" || current.Len() > 0 {
			args = append(args, strings.ReplaceAll(part, `\'`, `'`))
		}
		current.Reset()
	}

	for _, r := range raw {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			current.WriteRune(r)
			escaped = true
		case quote != 0:
			current.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			current.WriteRune(r)
			quote = r
		case r == ',':
			flush()
		default:
			current.WriteRune(r)
		}
	}

	flush()
	return args
}

func buildFeedbackCallPayload(args []string) string {
	values := url.Values{}
	keys := []string{
		"CourseName",
		"FacultyName",
		"StaffCode",
		"FacultyId",
		"iSRNO",
		"sType",
		"iDetId",
	}

	for i, key := range keys {
		if i >= len(args) {
			break
		}
		values.Set(key, strings.TrimSpace(args[i]))
	}

	return values.Encode()
}

func findCallEnd(raw string) int {
	var quote rune
	escaped := false

	for i, r := range raw {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ')':
			return i
		}
	}

	return -1
}

func escapeFeedbackValue(value string) string {
	if value == "" {
		return ""
	}
	return url.QueryEscape(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
