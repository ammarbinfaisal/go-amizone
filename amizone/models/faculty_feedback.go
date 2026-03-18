package models

type FacultyFeedbackSpecs []FacultyFeedbackSpec

type FacultyFeedbackSpec struct {
	VerificationToken string
	FeedbackEndpoint  string
	FeedbackMethod    string
	FeedbackPayload   string
	SubmitEndpoint    string

	CourseType   string
	DepartmentId string
	FacultyId    string
	SerialNumber string

	Set__Rating  string
	Set__QRating string
	Set__Comment string
}

type FacultyFeedbackSubmission struct {
	SubmitEndpoint string
	Payload        string
}
