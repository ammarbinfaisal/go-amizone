package models

// CourseRef is a model for representing a minimal reference to a course, usually embedded in other models.
type CourseRef struct {
	Code string
	Name string
}

// Courses is a model for representing a list of courses from the portal. This model
// should most often be used to hold all courses for a certain semester.
type Course struct {
	CourseRef
	Type          string
	Attendance    Attendance
	InternalMarks Marks  // 0, 0 if not available
	SyllabusDoc   string // Link to the course curriculum/syllabus page, when available.
}

type Courses []Course
