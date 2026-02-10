package parse

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/ditsuke/go-amizone/amizone/models"
	"k8s.io/klog/v2"
)

func Profile(body io.Reader) (*models.Profile, error) {
	dom, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", ErrFailedToParseDOM, err)
	}

	if !IsLoggedInDOM(dom) {
		return nil, errors.New(ErrNotLoggedIn)
	}

	// Check for "Not Applicable" response (ID card feature disabled)
	if isNotApplicablePage(dom) {
		klog.Info("parse(profile): ID Card feature not available (Not Applicable); returning empty profile")
		return &models.Profile{}, nil
	}

	if !isIDCardPage(dom) {
		// Fallback: maybe we're on the dashboard (some instances return full layouts).
		if info := dom.Find(selectorDashboardProfile); info.Length() > 0 {
			return parseDashboardProfile(dom)
		}

		breadcrumb := CleanString(dom.Find(selectorActiveBreadcrumb).Text())
		klog.Infof("parse(profile): ID Card page not available (breadcrumb: %q); returning empty profile", breadcrumb)
		return &models.Profile{}, nil
	}

	const (
		selectorCardFront = "#lblNameIDCardFront1"
		selectorCardBack  = "#lblInfoIDCardBack1"
		selectorHeadshot  = "img#ImgPhotoIDCardFront1"
	)

	name, course, batch := func() (string, string, string) {
		conDiv := dom.Find(selectorCardFront)
		// Replace <br>'s with newlines to make the semantic soup parsable
		conDiv.Find("br").ReplaceWithHtml("\n")
		all := CleanString(conDiv.Text())
		allSlice := strings.Split(all, "\n")
		if len(allSlice) != 3 {
			klog.Error("failed to parse out name, course and batch from the ID page")
			return "", "", ""
		}

		for i, s := range allSlice {
			allSlice[i] = CleanString(s)
		}

		return allSlice[0], allSlice[1], allSlice[2]
	}()

	// We now have some basic information to populate
	profile := &models.Profile{
		Name:    name,
		Program: course,
		Batch:   batch,
	}

	// Parse "SUID": a student UUID
	profile.UUID = func() string {
		headshotUrl, exists := dom.Find(selectorHeadshot).Attr("src")
		if !exists {
			klog.Warning("parse(profile): could not find profile student headshot URL")
			return ""
		}
		studentUUID := regexp.MustCompile(`\w{8}-\w{4}-\w{4}-\w{4}-\w{12}`).FindString(headshotUrl)
		if studentUUID == "" {
			klog.Warning("parse(profile): could not find student uuid in headshot URL")
		}
		return studentUUID
	}()

	const (
		lblEnrollmentNo = "Enrollment No"
		lblDOB          = "Date Of Birth"
		lblBloodGroup   = "Blood Group"
		lblValidity     = "Validity"
		lblCardNo       = "ID Card No"

		timeFormat = "02.01.2006"
	)

	// Parse stuff from "back" of the card
	backDiv := dom.Find(selectorCardBack)
	// replace <br>'s with newlines
	backDiv.Find("br").ReplaceWithHtml("\n")
	everything := strings.Split(
		CleanString(backDiv.Text()),
		"\n",
	)

	labelRegexp := regexp.MustCompile(`[\w .]+( )?:`)
	valueRegexp := regexp.MustCompile(`:( )?.*$`)

	for _, line := range everything {
		lbl := CleanString(labelRegexp.FindString(line), ':')
		value := CleanString(valueRegexp.FindString(line), ':')
		switch lbl {
		case lblEnrollmentNo:
			profile.EnrollmentNumber = value
		case lblDOB:
			dob, err := time.Parse(timeFormat, value)
			if err != nil {
				klog.Warningf("failed to parse DOB from ID card: %v", err)
				break
			}
			profile.DateOfBirth = dob
		case lblBloodGroup:
			profile.BloodGroup = value
		case lblValidity:
			validity, err := time.Parse(timeFormat, value)
			if err != nil {
				klog.Warningf("failed to parse validity from ID card: %v", err)
				break
			}
			profile.EnrollmentValidity = validity
		case lblCardNo:
			profile.IDCardNumber = value
		}
	}

	return profile, nil
}

func isIDCardPage(dom *goquery.Document) bool {
	const IDCardPageBreadcrumb = "ID Card View"
	return CleanString(dom.Find(selectorActiveBreadcrumb).Text()) == IDCardPageBreadcrumb
}

// isNotApplicablePage checks if the ID Card page returns "Not Applicable"
// (indicates the feature is disabled on this Amizone instance)
func isNotApplicablePage(dom *goquery.Document) bool {
	// Check for the specific "Not Applicable" message pattern
	text := CleanString(dom.Text())
	return strings.Contains(text, "Not Applicable")
}

func parseDashboardProfile(dom *goquery.Document) (*models.Profile, error) {
	info := dom.Find(selectorDashboardProfile)
	if info.Length() == 0 {
		return nil, errors.New("failed to find profile info on dashboard")
	}

	profile := &models.Profile{}

	// Text is like "Mr MALIK AMMAR FAISAL  A2305222014"
	// Small tag contains enrollment number
	profile.EnrollmentNumber = CleanString(info.Find("small").Text())

	// Full text minus small tag text should be the name
	infoClone := info.Clone()
	infoClone.Find("small").Remove()
	profile.Name = CleanString(infoClone.Text())

	// Try to get UUID from photo URL
	photoUrl, exists := dom.Find(selectorDashboardUserPhoto).Attr("src")
	if exists {
		studentUUID := regexp.MustCompile(`\w{8}-\w{4}-\w{4}-\w{4}-\w{12}`).FindString(photoUrl)
		profile.UUID = studentUUID
	}

	return profile, nil
}
