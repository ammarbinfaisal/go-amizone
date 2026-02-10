package parse_test

import (
	"strings"
	"testing"

	"github.com/ditsuke/go-amizone/amizone/internal/mock"
	"github.com/ditsuke/go-amizone/amizone/internal/parse"
	"github.com/ditsuke/go-amizone/amizone/models"
	. "github.com/onsi/gomega"
)

func TestCourses(t *testing.T) {
	testCases := []struct {
		name           string
		bodyFile       mock.File
		coursesMatcher func(g *GomegaWithT, courses models.Courses)
		errMatcher     func(g *GomegaWithT, err error)
	}{
		{
			name:     "current courses page",
			bodyFile: mock.CoursesPage,
			coursesMatcher: func(g *GomegaWithT, courses models.Courses) {
				g.Expect(courses).ToNot(BeNil())
				g.Expect(len(courses)).To(Equal(8))
			},
			errMatcher: func(g *GomegaWithT, err error) {
				g.Expect(err).ToNot(HaveOccurred())
			},
		},
		{
			name:     "semester wise courses page",
			bodyFile: mock.CoursesPageSemWise,
			coursesMatcher: func(g *GomegaWithT, courses models.Courses) {
				g.Expect(courses).ToNot(BeNil())
				g.Expect(len(courses)).To(Equal(8))
			},
			errMatcher: func(g *GomegaWithT, err error) {
				g.Expect(err).ToNot(HaveOccurred())
			},
		},
		{
			name:     "invalid courses page (login page)",
			bodyFile: mock.LoginPage,
			coursesMatcher: func(g *GomegaWithT, courses models.Courses) {
				g.Expect(courses).To(BeNil())
			},
			errMatcher: func(g *GomegaWithT, err error) {
				g.Expect(err.Error()).To(ContainSubstring(parse.ErrFailedToParse))
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			fileReader, err := testCase.bodyFile.Open()
			g.Expect(err).ToNot(HaveOccurred())
			courses, err := parse.Courses(fileReader)
			testCase.coursesMatcher(g, courses)
			testCase.errMatcher(g, err)
		})
	}
}

// TestCoursesInternalMarksFormats tests various internal marks formats
func TestCoursesInternalMarksFormats(t *testing.T) {
	testCases := []struct {
		name         string
		marksHTML    string
		expectedHave float32
		expectedMax  float32
	}{
		{
			name:         "new format with breakdown",
			marksHTML:    "20.40[20.40+0.00]/40.00",
			expectedHave: 20.4,
			expectedMax:  40.0,
		},
		{
			name:         "new format with bonus marks",
			marksHTML:    "50[49+1.00]/49",
			expectedHave: 50.0,
			expectedMax:  49.0,
		},
		{
			name:         "new format with split marks",
			marksHTML:    "27.5[25.5+2.00]/40",
			expectedHave: 27.5,
			expectedMax:  40.0,
		},
		{
			name:         "legacy format simple",
			marksHTML:    "20/40",
			expectedHave: 20.0,
			expectedMax:  40.0,
		},
		{
			name:         "legacy format with decimals",
			marksHTML:    "35.00[30.00+5.00]/40.00",
			expectedHave: 35.0,
			expectedMax:  40.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			// Create minimal HTML with just one course entry
			html := `<div id="CourseListSemWise"><div><table><thead><tr>
				<th>Course Code</th><th>Course Name</th><th>Type</th>
				<th>Attendance</th><th>Internal Asses.</th>
			</tr></thead><tbody><tr>
				<td data-title="Course Code">TEST101</td>
				<td data-title="Course Name">Test Course</td>
				<td data-title="Type">Compulsory</td>
				<td data-title="Attendance">10/10</td>
				<td data-title="Internal Asses.">` + tc.marksHTML + `</td>
			</tr></tbody></table></div></div>`

			courses, err := parse.Courses(strings.NewReader(html))
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(courses).To(HaveLen(1))

			course := courses[0]
			g.Expect(course.InternalMarks.Have).To(Equal(tc.expectedHave),
				"Have marks mismatch for format: %s", tc.marksHTML)
			g.Expect(course.InternalMarks.Max).To(Equal(tc.expectedMax),
				"Max marks mismatch for format: %s", tc.marksHTML)
		})
	}
}
