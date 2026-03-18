package parse

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/ditsuke/go-amizone/amizone/models"
)

var (
	feedbackAspectRatingFieldRe = regexp.MustCompile(`^FeedbackRating\[\d+\]\.Rating$`)
	feedbackQueryRatingFieldRe  = regexp.MustCompile(`^FeedbackRating_Q\d+Rating$`)
)

func FacultyFeedbackSubmission(body io.Reader, defaultSubmitEndpoint string, rating int32, queryRating int32, comment string) (models.FacultyFeedbackSubmission, error) {
	dom, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return models.FacultyFeedbackSubmission{}, fmt.Errorf("%s: %w", ErrFailedToParseDOM, err)
	}

	if !IsLoggedInDOM(dom) {
		return models.FacultyFeedbackSubmission{}, errors.New(ErrNotLoggedIn)
	}

	form := dom.Find("form").First()
	root := form
	if form.Length() == 0 {
		root = dom.Selection
	}

	submitEndpoint := strings.TrimSpace(firstNonEmpty(form.AttrOr("action", ""), defaultSubmitEndpoint))
	if submitEndpoint == "" {
		return models.FacultyFeedbackSubmission{}, errors.New(ErrFailedToParse)
	}

	values := url.Values{}
	seenField := make(map[string]struct{})
	ratingFields := 0

	root.Find("input[name], textarea[name], select[name]").Each(func(_ int, field *goquery.Selection) {
		name := strings.TrimSpace(field.AttrOr("name", ""))
		if name == "" {
			return
		}

		tagName := goquery.NodeName(field)
		fieldType := strings.ToLower(strings.TrimSpace(field.AttrOr("type", "")))

		switch {
		case feedbackAspectRatingFieldRe.MatchString(name):
			values.Set(name, fmt.Sprint(rating))
			seenField[name] = struct{}{}
			ratingFields++
			return
		case feedbackQueryRatingFieldRe.MatchString(name):
			values.Set(name, fmt.Sprint(queryRating))
			seenField[name] = struct{}{}
			return
		case strings.Contains(strings.ToLower(name), "comment"):
			values.Set(name, comment)
			seenField[name] = struct{}{}
			return
		}

		switch tagName {
		case "input":
			switch fieldType {
			case "radio", "checkbox":
				if _, already := seenField[name]; already {
					return
				}
				if _, checked := field.Attr("checked"); checked {
					values.Add(name, field.AttrOr("value", "on"))
					seenField[name] = struct{}{}
				}
			case "submit", "button", "reset", "file":
				return
			default:
				values.Set(name, field.AttrOr("value", ""))
				seenField[name] = struct{}{}
			}
		case "textarea":
			values.Set(name, field.Text())
			seenField[name] = struct{}{}
		case "select":
			selected := ""
			field.Find("option").EachWithBreak(func(_ int, option *goquery.Selection) bool {
				if _, ok := option.Attr("selected"); ok {
					selected = option.AttrOr("value", CleanString(option.Text()))
					return false
				}
				return true
			})
			if selected == "" {
				selected = field.Find("option").First().AttrOr("value", "")
			}
			values.Set(name, selected)
			seenField[name] = struct{}{}
		}
	})

	if ratingFields == 0 {
		return models.FacultyFeedbackSubmission{}, errors.New(ErrFailedToParse)
	}

	values.Set("X-Requested-With", "XMLHttpRequest")

	return models.FacultyFeedbackSubmission{
		SubmitEndpoint: submitEndpoint,
		Payload:        values.Encode(),
	}, nil
}
