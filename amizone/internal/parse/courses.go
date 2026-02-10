package parse

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/ditsuke/go-amizone/amizone/models"
	"k8s.io/klog/v2"
)

// Expose these data-title attributes, because they're used by the isCoursesPage function.
const (
	dtCourseCode       = "Course Code"
	dtCourseAttendance = "Attendance"
)

// Courses parses the Amizone courses page.
func Courses(body io.Reader) (models.Courses, error) {
	// selectors
	const (
		selectorPrimaryCourseTable   = "div:nth-child(1) > table:nth-child(1)"
		selectorSecondaryCourseTable = "div:nth-child(2) > table:nth-child(1)"
	)

	// "data-title" attributes for the primary course table
	const (
		dtCode        = dtCourseCode
		dtName        = "Course Name"
		dtType        = "Type"
		dtSyllabusDoc = "Course Syllabus"
		dtAttendance  = dtCourseAttendance
		dtInternals   = "Internal Asses."
	)

	dom, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ErrFailedToParseDOM, err)
	}

	if !IsLoggedInDOM(dom) {
		return nil, errors.New(ErrNotLoggedIn)
	}

	// We check for the course page first, but we can't rely on it alone because the "semester wise" course page does
	// not come with breadcrumbs.
	if !isCoursesPage(dom) {
		return nil, errors.New(ErrFailedToParse)
	}

	normDom := normalisePage(dom.Selection)

	courseTablePrimary := normDom.Find(selectorPrimaryCourseTable)
	if matches := courseTablePrimary.Length(); matches != 1 {
		klog.Warning("failed to find the main course table. selector matches:", matches)
		return nil, errors.New(ErrFailedToParse)
	}

	// primary courses
	primaryEntries := courseTablePrimary.Find(selectorDataRows)
	if primaryEntries.Length() == 0 {
		klog.Errorf("found no primary courses on the courses page")
		return nil, errors.New(ErrFailedToParse)
	}

	// secondary courses
	secondaryEntries := normDom.Find(selectorSecondaryCourseTable).Find(selectorDataRows)

	// all courses
	courseEntries := primaryEntries.AddSelection(secondaryEntries)

	// Build up our entries
	courses := make(models.Courses, courseEntries.Length())
	courseEntries.Each(func(i int, row *goquery.Selection) {
		course := models.Course{
			CourseRef: models.CourseRef{
				Name: CleanString(row.Find(fmt.Sprintf(selectorTplDataCell, dtName)).Text()),
				Code: CleanString(row.Find(fmt.Sprintf(selectorTplDataCell, dtCode)).Text()),
			},
				Type: CleanString(row.Find(fmt.Sprintf(selectorTplDataCell, dtType)).Text()),
				Attendance: func() models.Attendance {
					raw := row.Find(fmt.Sprintf(selectorTplDataCell, dtAttendance)).Text()
					cleanRaw := CleanString(raw)

					// Handle "NA" or empty attendance (common when attendance not yet available)
					if isNAValue(cleanRaw) {
						return models.Attendance{}
					}

					// Common format: "33/43 (76.74)"
					m := regexp.MustCompile(`(\d+)\s*/\s*(\d+)`).FindStringSubmatch(cleanRaw)
					if len(m) < 3 {
						// Some campuses show button text like "View" or "Not Published"
						if !isNonNumericValue(cleanRaw) {
							klog.Warningf("parse(courses): attendance string has unexpected format: %q", raw)
						}
						return models.Attendance{}
					}

					attended, err1 := strconv.Atoi(m[1])
					total, err2 := strconv.Atoi(m[2])
					if err1 != nil || err2 != nil {
						klog.Warningf("parse(courses): attendance parse error: %q (attended: %v, total: %v)", raw, err1, err2)
						return models.Attendance{}
					}
					return models.Attendance{
						ClassesAttended: int32(attended),
						ClassesHeld:     int32(total),
					}
				}(),
				InternalMarks: func() models.Marks {
					raw := row.Find(fmt.Sprintf(selectorTplDataCell, dtInternals)).Text()
					cleanRaw := CleanString(raw)

					// Handle empty marks field (common when marks not yet published)
					if isNAValue(cleanRaw) || isNonNumericValue(cleanRaw) {
						return models.Marks{}
					}

					// Marks can be in formats:
					// "15/20"
					// "15.5/20"
					// "15 [20]"
					// "15/20 (75.00)"
					// "20.40[20.40+0.00]/40.00" - new format with breakdown

					// Try the new format first: have[breakdown]/max
					// Example: 20.40[20.40+0.00]/40.00
					newFormat := regexp.MustCompile(`(\d+(?:\.\d+)?)\[[\d\.\+]+\]/(\d+(?:\.\d+)?)`).FindStringSubmatch(cleanRaw)
					if len(newFormat) >= 3 {
						have, err1 := strconv.ParseFloat(newFormat[1], 32)
						max, err2 := strconv.ParseFloat(newFormat[2], 32)
						if err1 != nil || err2 != nil {
							klog.Warningf("parse(courses): error in parsing marks (new format): %q (have: %v, max: %v)", raw, err1, err2)
							return models.Marks{}
						}
						return models.Marks{Max: float32(max), Have: float32(have)}
					}

					// Legacy format: "have/max" or "have [max]"
					pair := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:/|\[)\s*(\d+(?:\.\d+)?)`).FindStringSubmatch(cleanRaw)
					if len(pair) >= 3 {
						have, err1 := strconv.ParseFloat(pair[1], 32)
						max, err2 := strconv.ParseFloat(pair[2], 32)
						if err1 != nil || err2 != nil {
							klog.Warningf("parse(courses): error in parsing marks: %q (have: %v, max: %v)", raw, err1, err2)
							return models.Marks{}
						}
						return models.Marks{Max: float32(max), Have: float32(have)}
					}

					// Fallback: single numeric value.
					gotStr := regexp.MustCompile(`\d+(?:\.\d+)?`).FindString(cleanRaw)
					if gotStr == "" {
						return models.Marks{}
					}
					got, err := strconv.ParseFloat(gotStr, 32)
					if err != nil {
						klog.Warningf("parse(courses): error in parsing marks: %q (got: %v)", raw, err)
						return models.Marks{}
					}
					return models.Marks{Have: float32(got)}
				}(),
				SyllabusDoc: row.Find(fmt.Sprintf(selectorTplDataCell, dtSyllabusDoc)).Find("a").AttrOr("href", ""),
			}
			courses[i] = course
		})

	return courses, nil
}

func isCoursesPage(dom *goquery.Document) bool {
	const coursePageBreadcrumb = "My Courses"

	return dom.Find(selectorActiveBreadcrumb).Text() == coursePageBreadcrumb ||
		(dom.Find(fmt.Sprintf(selectorTplDataCell, dtCourseCode)).Length() != 0 &&
			dom.Find(fmt.Sprintf(selectorTplDataCell, dtCourseAttendance)).Length() != 0)
}

// normalisePage attempts to "normalise" the page by extracting the contexts of the "#CourseListSemWise" div.
// We need to do this because the page comes in two flavors: one when it has breadcrumbs and the course tables wrapped
// in the "#CourseListSemWise" div, and one when it doesn't (when we query courses for a non-current semester).
func normalisePage(dom *goquery.Selection) *goquery.Selection {
	if child := dom.Find("#CourseListSemWise").Children(); child.Length() > 0 {
		return child
	}
	return dom
}

func isNAValue(s string) bool {
	if s == "" {
		return true
	}
	normal := strings.ToUpper(strings.TrimSpace(s))
	normal = strings.ReplaceAll(normal, ".", "")
	normal = strings.ReplaceAll(normal, " ", "")
	switch normal {
	case "NA", "N/A":
		return true
	case "-", "--":
		return true
	default:
		return false
	}
}

func isNonNumericValue(s string) bool {
	upper := strings.ToUpper(s)
	return strings.Contains(upper, "NOT PUBLISHED") ||
		strings.Contains(upper, "NOT AVAILABLE") ||
		strings.Contains(upper, "NOT APPLICABLE") ||
		strings.Contains(upper, "VIEW")
}
