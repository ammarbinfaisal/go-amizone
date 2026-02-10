package amizone

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"text/template"
	"time"

	"k8s.io/klog/v2"

	"github.com/ditsuke/go-amizone/amizone/capsolver"
	"github.com/ditsuke/go-amizone/amizone/internal"
	"github.com/ditsuke/go-amizone/amizone/internal/marshaller"
	"github.com/ditsuke/go-amizone/amizone/internal/parse"
	"github.com/ditsuke/go-amizone/amizone/internal/validator"
	"github.com/ditsuke/go-amizone/amizone/models"
	"github.com/ditsuke/go-amizone/amizone/tlsclient"
)

// Endpoints
const (
	BaseURL = "https://" + internal.AmizoneDomain

	loginRequestEndpoint             = "/"
	attendancePageEndpoint           = "/Home"
	scheduleEndpointTemplate         = "/Calendar/home/GetDiaryEvents?start=%s&end=%s"
	examScheduleEndpoint             = "/Examination/ExamSchedule"
	currentCoursesEndpoint           = "/Academics/MyCourses"
	coursesEndpoint                  = currentCoursesEndpoint + "/CourseListSemWise"
	profileEndpoint                  = "/IDCard"
	macBaseEndpoint                  = "/RegisterForWifi/mac"
	currentExaminationResultEndpoint = "/Examination/Examination"
	examinationResultEndpoint        = currentExaminationResultEndpoint + "/ExaminationListSemWise"
	getWifiMacsEndpoint              = macBaseEndpoint + "/MacRegistration"
	registerWifiMacsEndpoint         = macBaseEndpoint + "/MacRegistrationSave"

	// deleteWifiMacEndpoint is peculiar in that it requires the user's ID as a parameter.
	// This _might_ open doors for an exploit (spoiler: indeed it does)
	removeWifiMacEndpoint = macBaseEndpoint + "/Mac1RegistrationDelete?Amizone_Id=%s&username=%s&X-Requested-With=XMLHttpRequest"

	facultyBaseEndpoint           = "/FacultyFeeback/FacultyFeedback"
	facultyEndpointSubmitEndpoint = facultyBaseEndpoint + "/SaveFeedbackRating"
)

// Miscellaneous
const (
	classScheduleEndpointDateFormat = "2006-01-02"

	verificationTokenName = "__RequestVerificationToken"
)

// Errors
const (
	ErrBadClient              = "the http client passed must have a cookie jar, or be nil"
	ErrFailedToVisitPage      = "failed to visit page"
	ErrFailedToFetchPage      = "failed to fetch page"
	ErrFailedToReadResponse   = "failed to read response body"
	ErrFailedLogin            = "failed to login"
	ErrInvalidCredentials     = ErrFailedLogin + ": invalid credentials"
	ErrInternalFailure        = "internal failure"
	ErrFailedToComposeRequest = ErrInternalFailure + ": failed to compose request"
	ErrFailedToParsePage      = ErrInternalFailure + ": failed to parse page"
	ErrInvalidMac             = "invalid MAC address passed"
	ErrNoMacSlots             = "no free wifi mac slots"
	ErrFailedToRegisterMac    = "failed to register mac address"
)

type Credentials struct {
	Username string
	Password string
}

// ClientOption is a function that configures a Client
type ClientOption func(*Client) error

// WithTLSClient enables TLS fingerprinting and browser impersonation
// This option creates an HTTP client that mimics real browsers to avoid detection
// by websites that use TLS fingerprinting. It supports profile rotation for
// increased resilience against bot detection.
//
// Example:
//
//	client, err := NewClientWithOptions(cred, WithTLSClient(nil))
func WithTLSClient(tlsOpts *tlsclient.ClientOptions) ClientOption {
	return func(c *Client) error {
		httpClient, err := tlsclient.NewHTTPClient(tlsOpts)
		if err != nil {
			return fmt.Errorf("failed to create TLS client: %w", err)
		}
		c.httpClient = httpClient
		return nil
	}
}

// WithCapSolver enables automatic CAPTCHA solving using CapSolver
// This option configures the client to automatically solve Cloudflare Turnstile
// and reCAPTCHA challenges during login using the CapSolver API.
//
// Example:
//
//	client, err := NewClientWithOptions(cred, WithCapSolver("your-api-key"))
func WithCapSolver(apiKey string) ClientOption {
	return func(c *Client) error {
		if apiKey == "" {
			return errors.New("CapSolver API key cannot be empty")
		}
		c.capsolverClient = capsolver.NewClient(apiKey)
		return nil
	}
}

// Client is the main struct for the amizone package, exposing the entire API surface
// for the portal as implemented here. The struct must always be initialized through a public
// constructor like NewClient()
type Client struct {
	httpClient      *http.Client
	credentials     *Credentials
	capsolverClient *capsolver.Client
	// muLogin is a mutex that protects login-related fields.
	muLogin struct {
		sync.Mutex
		lastAttempt      time.Time
		lastLoginSuccess time.Time
		didLogin         bool
	}
}

// DidLogin returns true if the client ever successfully logged in.
func (a *Client) DidLogin() bool {
	a.muLogin.Lock()
	defer a.muLogin.Unlock()
	return a.muLogin.didLogin
}

// NewClient create a new client instance with Credentials passed, then attempts to log in to the website.
// The *http.Client parameter can be nil, in which case a default client will be created in its place.
// To get a non-logged in client, pass empty credentials, ala Credentials{}.
//
// For advanced options including TLS fingerprinting, use NewClientWithOptions instead.
func NewClient(cred Credentials, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			klog.Error("failed to create cookiejar for the amizone client. this is a bug.")
			return nil, errors.New(ErrInternalFailure)
		}
		httpClient = &http.Client{Jar: jar}
	}

	if jar := httpClient.Jar; jar == nil {
		klog.Error("amizone.NewClient called with a jar-less http client. please pass a client with a non-nil cookie jar")
		return nil, errors.New(ErrBadClient)
	}

	client := &Client{
		httpClient:  httpClient,
		credentials: &cred,
	}

	if cred == (Credentials{}) {
		return client, nil
	}

	return client, client.login(false)
}

// NewClientWithOptions creates a new client with functional options.
// This allows for more flexible configuration, including TLS fingerprinting support.
//
// Example with TLS fingerprinting:
//
//	client, err := NewClientWithOptions(
//	    cred,
//	    WithTLSClient(&tlsclient.ClientOptions{
//	        ProfileRotationMode: tlsclient.ProfileRotationRandom,
//	        Timeout:            30 * time.Second,
//	    }),
//	)
//
// Example with default TLS fingerprinting:
//
//	client, err := NewClientWithOptions(cred, WithTLSClient(nil))
//
// If no options are provided, behaves the same as NewClient(cred, nil).
func NewClientWithOptions(cred Credentials, opts ...ClientOption) (*Client, error) {
	// Start with default HTTP client
	jar, err := cookiejar.New(nil)
	if err != nil {
		klog.Error("failed to create cookiejar for the amizone client. this is a bug.")
		return nil, errors.New(ErrInternalFailure)
	}

	client := &Client{
		httpClient:  &http.Client{Jar: jar},
		credentials: &cred,
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(client); err != nil {
			return nil, fmt.Errorf("failed to apply client option: %w", err)
		}
	}

	// Ensure the client has a cookie jar after options are applied
	if client.httpClient.Jar == nil {
		klog.Error("client option removed the cookie jar. this is not supported.")
		return nil, errors.New(ErrBadClient)
	}

	// Skip login for empty credentials
	if cred == (Credentials{}) {
		return client, nil
	}

	return client, client.login(false)
}

// login attempts to log in to Amizone. If force is false, it will attempt to reuse existing
// sessions if they appear valid and were established within the last hour.
func (a *Client) login(force bool) error {
	a.muLogin.Lock()
	defer a.muLogin.Unlock()

	// If not forced, check if we can reuse the current session.
	if !force {
		// Check if we have valid-looking cookies and a recent successful login.
		if internal.IsLoggedIn(a.httpClient) && time.Since(a.muLogin.lastLoginSuccess) < time.Hour {
			klog.V(1).Infof("login: reusing session (last success: %v ago)", time.Since(a.muLogin.lastLoginSuccess))
			a.muLogin.didLogin = true
			return nil
		}

		if time.Since(a.muLogin.lastAttempt) < time.Minute*2 {
			klog.Warning("login: last attempt was less than 2 minutes ago, skipping to avoid hammering")
			if a.muLogin.didLogin {
				return nil
			}
			return errors.New("login throttled")
		}
	}

	// Record our last login attempt so that we can avoid trying again for some time.
	a.muLogin.lastAttempt = time.Now()

	// Fetch the login page to get form fields and check for CAPTCHA requirements
	response, err := a.doRequest(false, http.MethodGet, "/", nil)
	if err != nil {
		klog.Errorf("login: %s", err.Error())
		return fmt.Errorf("%s: %w", ErrFailedLogin, err)
	}

	// Parse login form to get all required fields
	loginForm, err := parse.ParseLoginForm(response.Body)
	if err != nil {
		klog.Error("login: failed to parse login form")
		return fmt.Errorf("%s: %s", ErrFailedLogin, ErrFailedToParsePage)
	}

	if loginForm.VerificationToken == "" {
		klog.Error("login: failed to retrieve verification token from the login page")
		return fmt.Errorf("%s: %s", ErrFailedLogin, ErrFailedToParsePage)
	}

	// Prepare login form data
	loginRequestData := url.Values{}
	loginRequestData.Set(verificationTokenName, loginForm.VerificationToken)
	loginRequestData.Set("_UserName", a.credentials.Username)
	loginRequestData.Set("_Password", a.credentials.Password)
	loginRequestData.Set("_QString", "") // Will be set to "test" when CAPTCHA is solved
	loginRequestData.Set("honeypot", "") // Must be empty (anti-bot field)

	// Add any additional fields that were parsed
	if loginForm.Salt != "" {
		loginRequestData.Set("Salt", loginForm.Salt)
	}
	if loginForm.SecretNumber != "" {
		loginRequestData.Set("SecretNumber", loginForm.SecretNumber)
	}
	if loginForm.Signature != "" {
		loginRequestData.Set("Signature", loginForm.Signature)
	}
	if loginForm.Challenge != "" {
		loginRequestData.Set("Challenge", loginForm.Challenge)
	}

	// Solve CAPTCHA if CapSolver is configured
	klog.Infof("DEBUG: capsolverClient=%v, TurnstileSiteKey=%q", a.capsolverClient != nil, loginForm.TurnstileSiteKey)
	if a.capsolverClient != nil {
		klog.Info("CapSolver is configured, checking for CAPTCHA challenges")

		// Check for Cloudflare Turnstile
		if loginForm.TurnstileSiteKey != "" {
			klog.Infof("Cloudflare Turnstile detected (sitekey: %s), solving with CapSolver", loginForm.TurnstileSiteKey)
			turnstileToken, err := a.capsolverClient.SolveTurnstile(BaseURL, loginForm.TurnstileSiteKey)
			if err != nil {
				klog.Errorf("Failed to solve Turnstile: %s", err.Error())
				return fmt.Errorf("%s: failed to solve Turnstile CAPTCHA: %w", ErrFailedLogin, err)
			}
			// Amizone stores Turnstile token in RecaptchaToken field and sets _QString to "test"
			loginRequestData.Set("RecaptchaToken", turnstileToken)
			loginRequestData.Set("_QString", "test")
			// Also set cf-turnstile-response for compatibility
			loginRequestData.Set("cf-turnstile-response", turnstileToken)
			klog.Infof("Turnstile token set in RecaptchaToken and _QString=test")
		}

		// Note: reCAPTCHA on password recovery form, not login form
		// If it appears on login form in the future, we can handle it here
	}

		// Avoid logging secrets (passwords, tokens, signatures) at info level.
		if klog.V(2).Enabled() {
			redacted := url.Values{}
			for key, values := range loginRequestData {
				if len(values) == 0 {
					continue
				}
				switch key {
				case "_Password", "RecaptchaToken", "cf-turnstile-response", verificationTokenName, "Signature", "Challenge", "Salt", "SecretNumber":
					redacted.Set(key, "<redacted>")
				default:
					redacted.Set(key, values[0])
				}
			}
			klog.V(2).Infof("login: sending request fields: %s", redacted.Encode())
		}
	loginResponse, err := a.doRequest(
		false,
		http.MethodPost,
		loginRequestEndpoint,
		strings.NewReader(loginRequestData.Encode()),
	)
	if err != nil {
		klog.Warningf("error while making HTTP request to the amizone login page: %s", err.Error())
		return fmt.Errorf("%s: %w", ErrFailedLogin, err)
	}

	klog.Infof("DEBUG: Login response URL: %s, Status: %s", loginResponse.Request.URL.String(), loginResponse.Status)

	// The login request should redirect our request to the home page with a 302 "found" status code.
	// If we're instead redirected to the login page, we've failed to log in because of invalid credentials
	if loginResponse.Request.URL.Path == loginRequestEndpoint {
		klog.Infof("DEBUG: Login failed - redirected back to login page")
		return errors.New(ErrInvalidCredentials)
	}

	if loggedIn := parse.IsLoggedIn(loginResponse.Body); !loggedIn {
		klog.Error(
			"login attempt failed as indicated by parsing the page returned after the login request, while the redirect indicated that it passed." +
				" this failure indicates that something broke between Amizone and go-amizone.",
		)
		return errors.New(ErrFailedLogin)
	}

	if !internal.IsLoggedIn(a.httpClient) {
		klog.Error(
			"login attempt failed as indicated by checking the cookies in the http client's cookie jar. this failure indicates that something has broken between" +
				" Amizone and go-amizone, possibly the cookies used by amizone for authentication.",
		)
		return errors.New(ErrFailedLogin)
	}

	a.muLogin.didLogin = true
	a.muLogin.lastLoginSuccess = time.Now()
	return nil
}

// GetAttendance retrieves, parses and returns attendance data from Amizone for courses the client user is enrolled in
// for their latest semester.
func (a *Client) GetAttendance() (models.AttendanceRecords, error) {
	response, err := a.doRequest(true, http.MethodGet, attendancePageEndpoint, nil)
	if err != nil {
		klog.Warningf("request (attendance): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	attendanceRecord, err := parse.Attendance(response.Body)
	if err != nil {
		klog.Errorf("parse (attendance): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return models.AttendanceRecords(attendanceRecord), nil
}

// GetExaminationResult retrieves, parses and returns a ExaminationResultRecords from Amizone for their latest semester
// for which the result is available
func (a *Client) GetCurrentExaminationResult() (*models.ExamResultRecords, error) {
	response, err := a.doRequest(true, http.MethodGet, currentExaminationResultEndpoint, nil)
	if err != nil {
		klog.Warningf("request (examination-result): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	examinationResultRecords, err := parse.ExaminationResult(response.Body)
	if err != nil {
		klog.Errorf("parse (examination-result): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return examinationResultRecords, nil
}

// GetExaminationResult retrieves, parses and returns a ExaminationResultRecords from Amizone for the semester referred by
// semesterRef. Semester references should be retrieved through GetSemesters, which returns a list of valid
// semesters with names and references.
func (a *Client) GetExaminationResult(semesterRef string) (*models.ExamResultRecords, error) {
	payload := url.Values{
		"sem": []string{semesterRef},
	}.Encode()

	response, err := a.doRequest(true, http.MethodPost, examinationResultEndpoint, strings.NewReader(payload))
	if err != nil {
		klog.Warningf("request (examination-result): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	examinationResultRecords, err := parse.ExaminationResult(response.Body)
	if err != nil {
		klog.Errorf("parse (examination-result): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return examinationResultRecords, nil
}

// GetClassSchedule retrieves, parses and returns class schedule data from Amizone.
// The date parameter is used to determine which schedule to retrieve, however as Amizone imposes arbitrary limits on the
// date range, as in scheduled for dates older than some months are not stored by Amizone, we have no way of knowing if a request will succeed.
func (a *Client) GetClassSchedule(year int, month time.Month, date int) (models.ClassSchedule, error) {
	timeFrom := time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
	timeTo := timeFrom.Add(time.Hour * 24)

	endpoint := fmt.Sprintf(
		scheduleEndpointTemplate,
		timeFrom.Format(classScheduleEndpointDateFormat),
		timeTo.Format(classScheduleEndpointDateFormat),
	)

	response, err := a.doRequest(true, http.MethodGet, endpoint, nil)
	if err != nil {
		klog.Warningf("request (schedule): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	classSchedule, err := parse.ClassSchedule(response.Body)
	if err != nil {
		klog.Errorf("parse (schedule): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrFailedToParsePage, err)
	}
	// Filter classes by start date, since might also return classes for the dates before/after the target date.
	scheduledClassesForTargetDate := classSchedule.FilterByDate(timeFrom)

	return models.ClassSchedule(scheduledClassesForTargetDate), nil
}

// GetExamSchedule retrieves, parses and returns exam schedule data from Amizone.
// Amizone only allows to retrieve the exam schedule for the current semester, and only close to the exam
// dates once the date sheets are out, so we don't take a parameter here.
func (a *Client) GetExamSchedule() (*models.ExaminationSchedule, error) {
	response, err := a.doRequest(true, http.MethodGet, examScheduleEndpoint, nil)
	if err != nil {
		klog.Warningf("request (exam schedule): %s", err.Error())
		return nil, errors.New(ErrFailedToVisitPage)
	}

	examSchedule, err := parse.ExaminationSchedule(response.Body)
	if err != nil {
		klog.Errorf("parse (exam schedule): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return (*models.ExaminationSchedule)(examSchedule), nil
}

// GetSemesters retrieves, parses and returns a SemesterList from Amizone. This list includes all semesters for which
// information can be retrieved through other semester-specific methods like GetCourses.
func (a *Client) GetSemesters() (models.SemesterList, error) {
	response, err := a.doRequest(true, http.MethodGet, currentCoursesEndpoint, nil)
	if err != nil {
		klog.Warningf("request (get semesters): %s", err.Error())
		return nil, errors.New(ErrFailedToVisitPage)
	}

	semesters, err := parse.Semesters(response.Body)
	if err != nil {
		klog.Errorf("parse (semesters): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return (models.SemesterList)(semesters), nil
}

// GetCourses retrieves, parses and returns a SemesterList from Amizone for the semester referred by
// semesterRef. Semester references should be retrieved through GetSemesters, which returns a list of valid
// semesters with names and references.
func (a *Client) GetCourses(semesterRef string) (models.Courses, error) {
	payload := url.Values{
		"sem": []string{semesterRef},
	}.Encode()

	response, err := a.doRequest(true, http.MethodPost, coursesEndpoint, strings.NewReader(payload))
	if err != nil {
		klog.Warningf("request (get courses): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	courses, err := parse.Courses(response.Body)
	if err != nil {
		klog.Errorf("parse (courses): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return models.Courses(courses), nil
}

// GetCurrentCourses retrieves, parses and returns a SemesterList from Amizone for the most recent semester.
func (a *Client) GetCurrentCourses() (models.Courses, error) {
	response, err := a.doRequest(true, http.MethodGet, currentCoursesEndpoint, nil)
	if err != nil {
		klog.Warningf("request (get current courses): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	courses, err := parse.Courses(response.Body)
	if err != nil {
		klog.Errorf("parse (current courses): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return models.Courses(courses), nil
}

// GetUserProfile retrieves, parsed and returns the current user's profile from Amizone.
func (a *Client) GetUserProfile() (*models.Profile, error) {
	response, err := a.doRequest(true, http.MethodGet, profileEndpoint, nil)
	if err != nil {
		klog.Warningf("request (get profile): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	profile, err := parse.Profile(response.Body)
	if err != nil {
		klog.Errorf("parse (profile): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return (*models.Profile)(profile), nil
}

func (a *Client) GetWiFiMacInformation() (*models.WifiMacInfo, error) {
	response, err := a.doRequest(true, http.MethodGet, getWifiMacsEndpoint, nil)
	if err != nil {
		klog.Warningf("request (get wifi macs): %s", err.Error())
		return nil, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	info, err := parse.WifiMacInfo(response.Body)
	if err != nil {
		klog.Errorf("parse (wifi macs): %s", err.Error())
		return nil, fmt.Errorf("%s: %w", ErrInternalFailure, err)
	}

	return (*models.WifiMacInfo)(info), nil
}

// RegisterWifiMac registers a mac address on Amizone.
// If bypassLimit is true, it bypasses Amizone's artificial 2-address
// limitation. However, only the 2 oldest mac addresses are reflected
// in the GetWifiMacInfo response.
// TODO: is the bypassLimit functional?
func (a *Client) RegisterWifiMac(addr net.HardwareAddr, bypassLimit bool) error {
	// validate
	err := validator.ValidateHardwareAddr(addr)
	if err != nil {
		return errors.New(ErrInvalidMac)
	}
	wifiInfo, err := a.GetWiFiMacInformation()
	if err != nil {
		klog.Warningf("failure while getting wifi mac info: %s", err.Error())
		return err
	}

	if wifiInfo.IsRegistered(addr) {
		klog.Infof("wifi already registered.. skipping request")
		return nil
	}

	if !wifiInfo.HasFreeSlot() {
		if !bypassLimit {
			return errors.New(ErrNoMacSlots)
		}
		// Remove the last mac address :)
		wifiInfo.RegisteredAddresses = wifiInfo.RegisteredAddresses[:len(wifiInfo.RegisteredAddresses)-1]
	}

	wifis := append(wifiInfo.RegisteredAddresses, addr)

	payload := url.Values{}
	payload.Set(verificationTokenName, wifiInfo.GetRequestVerificationToken())
	// ! VULN: register mac as anyone or no one by changing this ID.
	payload.Set("Amizone_Id", a.credentials.Username)

	// _Name_ is a dummy field, as in it doesn't matter what its value is, but it needs to be present.
	// I suspect this might go straight into the DB.
	payload.Set("Name", "DoesntMatter")

	for i, mac := range wifis {
		payload.Set(fmt.Sprintf("Mac%d", i+1), marshaller.Mac(mac))
	}

	res, err := a.doRequest(true, http.MethodPost, registerWifiMacsEndpoint, strings.NewReader(payload.Encode()))
	if err != nil {
		klog.Errorf("request (register wifi mac): %s", err.Error())
		return fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}
	// We attempt to verify if the mac was set successfully, but its futile if bypassLimit was used since Amizone only exposes
	if bypassLimit {
		return nil
	}

	macs, err := parse.WifiMacInfo(res.Body)
	if err != nil {
		klog.Errorf("parse (wifi macs): %s", err.Error())
		return errors.New(ErrFailedToParsePage)
	}
	if !macs.IsRegistered(addr) {
		klog.Errorf("mac not registered: %s", addr.String())
		return errors.New(ErrFailedToRegisterMac)
	}

	return nil
}

// RemoveWifiMac removes a mac address from the Amizone mac address registry. If the mac address is not registered in the
// first place, this function does nothing.
func (a *Client) RemoveWifiMac(addr net.HardwareAddr) error {
	err := validator.ValidateHardwareAddr(addr)
	if err != nil {
		return errors.New(ErrInvalidMac)
	}

	// ! VULN: remove mac addresses registered by anyone if you know the mac/username pair.
	response, err := a.doRequest(
		true,
		http.MethodGet,
		fmt.Sprintf(removeWifiMacEndpoint, a.credentials.Username, marshaller.Mac(addr)),
		nil,
	)
	if err != nil {
		klog.Errorf("request (remove wifi mac): %s", err.Error())
		return fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	wifiInfo, err := parse.WifiMacInfo(response.Body)
	if err != nil {
		klog.Errorf("parse (wifi macs): %s", err.Error())
		return errors.New(ErrFailedToParsePage)
	}

	if wifiInfo.IsRegistered(addr) {
		return errors.New("failed to remove mac address")
	}

	return nil
}

// SubmitFacultyFeedbackHack submits feedback for *all* faculties, giving the same ratings and comments to all.
// This is a hack because we're not allowing fine-grained control over feedback points or individual faculties. This is
// because the form is a pain to parse, and the feedback system is a pain to work with in general.
// Returns: the number of faculties for which feedback was submitted. Note that this number would be zero
// if the feedback was already submitted or is not open.
func (a *Client) SubmitFacultyFeedbackHack(rating int32, queryRating int32, comment string) (int32, error) {
	// Validate
	if rating > 5 || rating < 1 {
		return 0, errors.New("invalid rating")
	}
	if queryRating > 3 || queryRating < 1 {
		return 0, errors.New("invalid query rating")
	}
	if comment == "" {
		return 0, errors.New("comment cannot be empty")
	}

	// Transform queryRating for "higher number is higher rating" semantics (it's the opposite in the form 😭)
	switch queryRating {
	case 1:
		queryRating = 3
	case 3:
		queryRating = 1
	}

	facultyPage, err := a.doRequest(true, http.MethodGet, facultyBaseEndpoint, nil)
	if err != nil {
		klog.Errorf("request (faculty page): %s", err.Error())
		return 0, fmt.Errorf("%s: %s", ErrFailedToFetchPage, err.Error())
	}

	feedbackSpecs, err := parse.FacultyFeedback(facultyPage.Body)
	if err != nil {
		klog.Errorf("parse (faculty feedback): %s", err.Error())
		return 0, errors.New(ErrFailedToParsePage)
	}

	payloadTemplate, err := template.New("facultyFeedback").Parse(facultyFeedbackTpl)
	if err != nil {
		klog.Errorf("Error parsing faculty feedback template: %s", err.Error())
		return 0, errors.New(ErrInternalFailure)
	}

	// Parallelize feedback submission for max gains 📈
	wg := sync.WaitGroup{}
	for _, spec := range feedbackSpecs {
		spec.Set__Rating = fmt.Sprint(rating)
		spec.Set__Comment = url.QueryEscape(comment)
		spec.Set__QRating = fmt.Sprint(queryRating)

		payloadBuilder := strings.Builder{}
		err = payloadTemplate.Execute(&payloadBuilder, spec)
		if err != nil {
			klog.Errorf("Error executing faculty feedback template: %s", err.Error())
			return 0, fmt.Errorf("error marshalling feedback request: %s", err)
		}
		wg.Add(1)
		go func(payload string) {
			response, err := a.doRequest(true, http.MethodPost, facultyEndpointSubmitEndpoint, strings.NewReader(payload))
			if err != nil {
				klog.Errorf("error submitting a faculty feedback: %s", err.Error())
			}
			if response.StatusCode != http.StatusOK {
				klog.Errorf("Unexpected non-200 status code from faculty feedback submission: %d", response.StatusCode)
			}
			wg.Done()
		}(payloadBuilder.String())
	}

	wg.Wait()
	return int32(len(feedbackSpecs)), nil
}
