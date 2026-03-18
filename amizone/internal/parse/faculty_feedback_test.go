package parse_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ditsuke/go-amizone/amizone/internal/mock"
	"github.com/ditsuke/go-amizone/amizone/internal/parse"
	. "github.com/onsi/gomega"
)

func TestFacultyFeedback(t *testing.T) {
	g := NewWithT(t)
	r, err := mock.FacultyPage.Open()
	g.Expect(err).ToNot(HaveOccurred())

	spec, err := parse.FacultyFeedback(r)
	g.Expect(err).ToNot(HaveOccurred())

	expected := ReadExpectedFile(mock.ExpectedFacultyFeedbackSpec, g)
	g.Expect(toJSON(spec, g)).To(MatchJSON(expected))
}

func TestFacultyFeedbackParsesFunctionCallEntries(t *testing.T) {
	g := NewWithT(t)
	html := `
		<div class="breadcrumbs"><ul class="breadcrumb"><li class="active">My Faculty</li></ul></div>
		<script>
			function FnFeedBackAgian(CourseName, FacultyName, StaffCode, FacultyId, iSRNO, SType, iDetId) {
				$.ajax({ url: '/FacultyFeeback/SummerSemesterFacultyFeedback/_FeedbackRating' });
			}
		</script>
		<input name="__RequestVerificationToken" type="hidden" value="token-value" />
		<button onclick="FnFeedBackAgian('Artificial Intelligence [CSE401]', 'Prof.(Dr) Sanjay Kumar Dubey', '2436', '1248826', '1730207', 'General', '94480')">Feedback</button>
	`

	spec, err := parse.FacultyFeedback(strings.NewReader(html))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(spec).To(HaveLen(1))
	g.Expect(spec[0].FeedbackEndpoint).To(Equal("/FacultyFeeback/SummerSemesterFacultyFeedback/_FeedbackRating"))
	g.Expect(spec[0].FeedbackMethod).To(Equal("POST"))
	payload, err := url.ParseQuery(spec[0].FeedbackPayload)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(payload.Get("CourseName")).To(Equal("Artificial Intelligence [CSE401]"))
	g.Expect(payload.Get("FacultyName")).To(Equal("Prof.(Dr) Sanjay Kumar Dubey"))
	g.Expect(payload.Get("StaffCode")).To(Equal("2436"))
	g.Expect(spec[0].SubmitEndpoint).To(Equal("/FacultyFeeback/SummerSemesterFacultyFeedback/SaveFeedbackRating"))
	g.Expect(spec[0].FacultyId).To(Equal("1248826"))
	g.Expect(spec[0].SerialNumber).To(Equal("1730207"))
	g.Expect(spec[0].CourseType).To(Equal("General"))
	g.Expect(spec[0].DepartmentId).To(Equal("94480"))
}

func TestFacultyFeedbackSubmissionBuildsDynamicPayload(t *testing.T) {
	g := NewWithT(t)
	html := `
		<form action="/FacultyFeeback/FacultyFeedback/SaveFeedbackRating" method="post">
			<input name="__RequestVerificationToken" type="hidden" value="token-value" />
			<input name="CourseType" type="hidden" value="General" />
			<input name="clsCourseFaculty.iDetId" type="hidden" value="94480" />
			<input name="clsCourseFaculty.iFacultyStaffId" type="hidden" value="1248826" />
			<input name="clsCourseFaculty.iSRNO" type="hidden" value="1730207" />
			<input name="FeedbackRating[0].iAspectId" type="hidden" value="1" />
			<input name="FeedbackRating[0].Rating" type="radio" value="1" />
			<input name="FeedbackRating[0].Rating" type="radio" value="5" checked />
			<input name="FeedbackRating[1].iAspectId" type="hidden" value="2" />
			<input name="FeedbackRating[1].Rating" type="radio" value="3" checked />
			<select name="FeedbackRating_Q1Rating">
				<option value="1">1</option>
				<option value="2" selected>2</option>
				<option value="3">3</option>
			</select>
			<textarea name="FeedbackRating_Comments">old comment</textarea>
		</form>
	`

	submission, err := parse.FacultyFeedbackSubmission(strings.NewReader(html), "", 4, 2, "fresh comment")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(submission.SubmitEndpoint).To(Equal("/FacultyFeeback/FacultyFeedback/SaveFeedbackRating"))

	values, err := url.ParseQuery(submission.Payload)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(values.Get("__RequestVerificationToken")).To(Equal("token-value"))
	g.Expect(values.Get("CourseType")).To(Equal("General"))
	g.Expect(values.Get("FeedbackRating[0].iAspectId")).To(Equal("1"))
	g.Expect(values.Get("FeedbackRating[0].Rating")).To(Equal("4"))
	g.Expect(values.Get("FeedbackRating[1].Rating")).To(Equal("4"))
	g.Expect(values.Get("FeedbackRating_Q1Rating")).To(Equal("2"))
	g.Expect(values.Get("FeedbackRating_Comments")).To(Equal("fresh comment"))
	g.Expect(values.Get("X-Requested-With")).To(Equal("XMLHttpRequest"))
}
