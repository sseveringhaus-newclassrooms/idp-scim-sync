package aws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Consume http methods
// implement scim.AWSSCIMProvider interface

// AWS SSO SCIM API
// reference: https://docs.aws.amazon.com/singlesignon/latest/developerguide/what-is-scim.html

var (
	// ErrURLEmpty is returned when the URL is empty.
	ErrURLEmpty = errors.Errorf("aws: url may not be empty")

	// ErrDisplayNameEmpty is returned when the display name is empty.
	ErrDisplayNameEmpty = errors.Errorf("aws: display name may not be empty")

	// ErrGivenNameEmpty is returned when the given name is empty.
	ErrGivenNameEmpty = errors.Errorf("aws: given name may not be empty")

	// ErrFamilyNameEmpty is returned when the family name is empty.
	ErrFamilyNameEmpty = errors.Errorf("aws: family name may not be empty")

	// ErrEmailsTooMany is returned when the emails has more than one entity.
	ErrEmailsTooMany = errors.Errorf("aws: emails may not be more than 1")

	// ErrCreateGroupRequestEmpty is returned when the create group request is empty.
	ErrCreateGroupRequestEmpty = errors.Errorf("aws: create group request may not be empty")

	// ErrCreateUserRequestEmpty is returned when the create user request is empty.
	ErrCreateUserRequestEmpty = errors.Errorf("aws: create user request may not be empty")

	// ErrPatchGroupRequestEmpty is returned when the patch group request is empty.
	ErrPatchGroupRequestEmpty = errors.Errorf("aws: patch group request may not be empty")

	// ErrGroupIDEmpty is returned when the group id is empty.
	ErrGroupIDEmpty = errors.Errorf("aws: group id may not be empty")

	// ErrUserIDEmpty is returned when the user id is empty.
	ErrUserIDEmpty = errors.Errorf("aws: user id may not be empty")

	// ErrPatchUserRequestEmpty is returned when the patch user request is empty.
	ErrPatchUserRequestEmpty = errors.Errorf("aws: patch user request may not be empty")

	// ErrPutUserRequestEmpty is returned when the put user request is empty.
	ErrPutUserRequestEmpty = errors.Errorf("aws: put user request may not be empty")

	// ErrUserNameEmpty is returned when the user name is empty.
	ErrUserNameEmpty = errors.Errorf("aws: user name may not be empty")

	// ErrUserUserNameEmpty is returned when the userName is empty.
	ErrUserUserNameEmpty = errors.Errorf("aws: userName may not be empty")

	// ErrGroupDisplayNameEmpty is returned when the userName is empty.
	ErrGroupDisplayNameEmpty = errors.Errorf("aws: displayName may not be empty")
)

//go:generate go run github.com/golang/mock/mockgen@v1.6.0 -package=mocks -destination=../../mocks/aws/scim_mocks.go -source=scim.go HTTPClient

// HTTPClient is an interface for sending HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// SCIMService is an AWS SCIM Service.
type SCIMService struct {
	httpClient  HTTPClient
	url         *url.URL
	UserAgent   string
	bearerToken string
}

// NewSCIMService creates a new AWS SCIM Service.
func NewSCIMService(httpClient HTTPClient, urlStr, token string) (*SCIMService, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	if urlStr == "" {
		return nil, ErrURLEmpty
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("aws: error parsing url: %w", err)
	}

	return &SCIMService{
		httpClient:  httpClient,
		url:         u,
		bearerToken: token,
	}, nil
}

// newRequest creates an http.Request with the given method, URL, and (optionally) body.
func (s *SCIMService) newRequest(ctx context.Context, method string, u *url.URL, body interface{}) (*http.Request, error) {
	var buf io.ReadWriter
	if body != nil {
		buf = &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)

		err := enc.Encode(body)
		if err != nil {
			return nil, fmt.Errorf("aws: error encoding request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), buf)
	if err != nil {
		return nil, fmt.Errorf("aws: error creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/scim+json")
	}

	req.Header.Set("Accept", "application/json")

	if s.UserAgent != "" {
		req.Header.Set("User-Agent", s.UserAgent)
	}
	return req, nil
}

// do sends an HTTP request and returns an HTTP response, following policy (e.g. redirects, cookies, auth) as configured on the client.
func (s *SCIMService) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)

	// Set bearer token
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.bearerToken))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws do: error sending request: %w", err)
	}

	return resp, nil
}

// checkHTTPResponse checks the status code of the HTTP response.
func (s *SCIMService) checkHTTPResponse(resp *http.Response) error {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("aws checkHTTPResponse: error reading response body: %w", err)
		}

		log.WithFields(log.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Tracef("aws checkHTTPResponse: body: %s\n", string(body))
		return &HTTPResponseError{resp.StatusCode, resp.Status, string(body)}
	}

	return nil
}

// CreateUser creates a new user in the AWS SSO Using the API.
// references:
// + https://docs.aws.amazon.com/singlesignon/latest/developerguide/createuser.html
func (s *SCIMService) CreateUser(ctx context.Context, usr *CreateUserRequest) (*CreateUserResponse, error) {
	if usr == nil {
		return nil, ErrCreateUserRequestEmpty
	}
	// Check constraints in the reference document
	if usr.UserName == "" {
		return nil, ErrUserNameEmpty
	}
	if usr.DisplayName == "" {
		return nil, ErrDisplayNameEmpty
	}
	if usr.Name.GivenName == "" {
		return nil, ErrGivenNameEmpty
	}
	if usr.Name.FamilyName == "" {
		return nil, ErrFamilyNameEmpty
	}

	if len(usr.Emails) > 1 {
		return nil, ErrEmailsTooMany
	}
	usr.Emails[0].Primary = true

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws CreateUser: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Users")

	req, err := s.newRequest(ctx, http.MethodPost, reqURL, *usr)
	if err != nil {
		return nil, fmt.Errorf("aws CreateUser: error creating request, user: %s, http method: %s, url: %v, error: %w", usr.UserName, http.MethodPost, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws CreateUser: error sending request, user: %s, http method: %s, url: %v, error: %w", usr.UserName, http.MethodPost, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var response CreateUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("aws CreateUser: user: %s, error decoding response body: %w", usr.UserName, err)
	}

	return &response, nil
}

// CreateOrGetUser creates a new user or get the user informaion in the AWS SSO Using the API.
// This function will try to create a new user but if received a 409 http error (ConflictException	User already exists.)
// execute a request to get the user information and return it.
//
// NOTE: this function is created to avoid the existing problem with the limitation of the
// AWS SCIM API about retrieve a maximum of 50 users at a time.
//
// references:
// + https://docs.aws.amazon.com/singlesignon/latest/developerguide/createuser.html
// + https://docs.aws.amazon.com/singlesignon/latest/developerguide/getuser.html
func (s *SCIMService) CreateOrGetUser(ctx context.Context, usr *CreateUserRequest) (*CreateUserResponse, error) {
	if usr == nil {
		return nil, ErrCreateUserRequestEmpty
	}
	// Check constraints in the reference document
	if usr.UserName == "" {
		return nil, ErrUserNameEmpty
	}
	if usr.DisplayName == "" {
		return nil, ErrDisplayNameEmpty
	}
	if usr.Name.GivenName == "" {
		return nil, ErrGivenNameEmpty
	}
	if usr.Name.FamilyName == "" {
		return nil, ErrFamilyNameEmpty
	}

	if len(usr.Emails) > 1 {
		return nil, ErrEmailsTooMany
	}
	usr.Emails[0].Primary = true

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws CreateUser: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Users")

	req, err := s.newRequest(ctx, http.MethodPost, reqURL, *usr)
	if err != nil {
		return nil, fmt.Errorf("aws CreateUser: error creating request, user: %s, http method: %s, url: %v, error: %w", usr.UserName, http.MethodPost, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws CreateUser: error sending request, user: %s, http method: %s, url: %v, error: %w", usr.UserName, http.MethodPost, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		httpErr := new(HTTPResponseError)

		// http.StatusConflict is 409
		if errors.As(e, &httpErr) && httpErr.StatusCode == http.StatusConflict {
			log.WithFields(log.Fields{
				"user": usr.UserName,
			}).Warn("aws CreateOrGetUser: user already exists, trying to get the user information")

			response, err := s.GetUserByUserName(ctx, usr.UserName)
			if err != nil {
				return nil, fmt.Errorf("aws CreateOrGetUser: error getting user information: %w", err)
			}

			log.WithFields(log.Fields{
				"user":       usr.UserName,
				"id":         response.ID,
				"externalId": response.ExternalID,
				"active":     response.Active,
			}).Trace("aws CreateOrGetUser: obtained user information")

			return &CreateUserResponse{
				ID:          response.ID,
				ExternalID:  response.ExternalID,
				Meta:        response.Meta,
				Schemas:     response.Schemas,
				UserName:    response.UserName,
				Name:        response.Name,
				DisplayName: response.DisplayName,
				Active:      response.Active,
				Emails:      response.Emails,
			}, nil
		}
		return nil, e
	}

	var response CreateUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("aws CreateOrGetUser: user: %s, error decoding response body: %w", usr.UserName, err)
	}

	return &response, nil
}

// DeleteUser deletes a user in the AWS SSO Using the API.
func (s *SCIMService) DeleteUser(ctx context.Context, id string) error {
	if id == "" {
		return ErrUserIDEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return fmt.Errorf("aws: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, fmt.Sprintf("/Users/%s", id))

	req, err := s.newRequest(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("aws: error creating request, http method: %s, url: %v, error: %w", http.MethodDelete, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return fmt.Errorf("aws: error sending request, http method: %s, url: %v, error: %w", http.MethodDelete, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		httpErr := new(HTTPResponseError)

		// http.StatusNotFound is 404
		if errors.As(e, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			log.WithFields(log.Fields{
				"user_id": id,
			}).Warnf("aws DeleteUser: user not found, operation discarded")
		}
		return e
	}
	return nil
}

// GetUserByUserName gets a user by username in the AWS SSO Using the API.
func (s *SCIMService) GetUserByUserName(ctx context.Context, userName string) (*GetUserResponse, error) {
	if userName == "" {
		return nil, ErrUserUserNameEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws GetUserByUserName: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Users")

	filter := fmt.Sprintf("userName eq %q", userName)
	q := reqURL.Query()
	q.Add("filter", filter)
	reqURL.RawQuery = q.Encode()

	req, err := s.newRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aws GetUserByUserName: error creating request, userName: %s, http method: %s, url: %v, error: %w", userName, http.MethodGet, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws GetUserByUserName: error sending request, userName: %s, http method: %s, url: %v, error: %w", userName, http.MethodGet, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var lur ListUsersResponse
	if err = json.NewDecoder(resp.Body).Decode(&lur); err != nil {
		return nil, fmt.Errorf("aws GetUserByUserName: userName: %s, error decoding response body: %w", userName, err)
	}

	var response GetUserResponse

	if len(lur.Resources) > 0 {
		dataJSON := lur.Resources[0].String()
		if err != nil {
			return nil, fmt.Errorf("aws GetUserByUserName: userName: %s, error decoding response body: %w", userName, err)
		}

		data := strings.NewReader(dataJSON)
		if err = json.NewDecoder(data).Decode(&response); err != nil {
			return nil, fmt.Errorf("aws GetUserByUserName: userName: %s, error decoding response body: %w", userName, err)
		}
	}

	return &response, nil
}

// GetUser returns an user from the AWS SSO Using the API
func (s *SCIMService) GetUser(ctx context.Context, userID string) (*GetUserResponse, error) {
	if userID == "" {
		return nil, ErrUserIDEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws GetUser: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, fmt.Sprintf("/Users/%s", userID))

	req, err := s.newRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aws GetUser: error creating request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws GetUser: error sending request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var response GetUserResponse
	if err = json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("aws GetUser: error decoding response body: %w", err)
	}

	return &response, nil
}

// ListUsers returns a list of users from the AWS SSO Using the API
func (s *SCIMService) ListUsers(ctx context.Context, filter string) (*ListUsersResponse, error) {
	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws ListUsers: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Users")

	if filter != "" {
		q := reqURL.Query()
		q.Add("filter", filter)
		reqURL.RawQuery = q.Encode()
	}

	req, err := s.newRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aws ListUsers: error creating request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws ListUsers: error sending request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var response ListUsersResponse
	if err = json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("aws ListUsers: error decoding response body: %w", err)
	}

	return &response, nil
}

// PatchUser updates a user in the AWS SSO Using the API
func (s *SCIMService) PatchUser(ctx context.Context, pur *PatchUserRequest) error {
	if pur == nil {
		return ErrPatchUserRequestEmpty
	}
	if pur.User.ID == "" {
		return ErrUserIDEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return fmt.Errorf("aws: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, fmt.Sprintf("/Groups/%s", pur.User.ID))

	req, err := s.newRequest(ctx, http.MethodPatch, reqURL, pur.Patch)
	if err != nil {
		return fmt.Errorf("aws: error creating request, http method: %s, url: %v, error: %w", http.MethodPatch, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return fmt.Errorf("aws: error sending request, http method: %s, url: %v, error: %w", http.MethodPatch, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return e
	}

	return nil
}

// PutUser creates a new user in the AWS SSO Using the API.
func (s *SCIMService) PutUser(ctx context.Context, usr *PutUserRequest) (*PutUserResponse, error) {
	if usr == nil {
		return nil, ErrPutUserRequestEmpty
	}
	// Check constraints in the reference document
	if usr.DisplayName == "" {
		return nil, ErrDisplayNameEmpty
	}
	if usr.Name.GivenName == "" {
		return nil, ErrGivenNameEmpty
	}
	if usr.Name.FamilyName == "" {
		return nil, ErrFamilyNameEmpty
	}

	if len(usr.Emails) > 1 {
		return nil, ErrEmailsTooMany
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws PutUser: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, fmt.Sprintf("/Users/%s", usr.ID))

	req, err := s.newRequest(ctx, http.MethodPut, reqURL, *usr)
	if err != nil {
		return nil, fmt.Errorf("aws PutUser: error creating request, http method: %s, url: %v, error: %w", http.MethodPut, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws PutUser: error sending request, http method: %s, url: %v, error: %w", http.MethodPut, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var response PutUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("aws PutUser: error decoding response body: %w", err)
	}

	return &response, nil
}

// GetGroupByDisplayName gets a group by display name from AWS SSO Using the API.
func (s *SCIMService) GetGroupByDisplayName(ctx context.Context, displayName string) (*GetGroupResponse, error) {
	if displayName == "" {
		return nil, ErrGroupDisplayNameEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws GetGroupByDisplayName: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Groups")

	filter := fmt.Sprintf("displayName eq %q", displayName)
	q := reqURL.Query()
	q.Add("filter", filter)
	reqURL.RawQuery = q.Encode()

	req, err := s.newRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aws GetGroupByDisplayName: error creating request, displayName: %s, http method: %s, url: %v, error: %w", displayName, http.MethodGet, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws GetGroupByDisplayName: error sending request, displayName: %s, http method: %s, url: %v, error: %w", displayName, http.MethodGet, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var lgr ListGroupsResponse
	if err = json.NewDecoder(resp.Body).Decode(&lgr); err != nil {
		return nil, fmt.Errorf("aws GetGroupByDisplayName: displayName: %s, error decoding response body: %w", displayName, err)
	}

	var response GetGroupResponse

	if len(lgr.Resources) > 0 {
		dataJSON := lgr.Resources[0].String()
		if err != nil {
			return nil, fmt.Errorf("aws GetGroupByDisplayName: displayName: %s, error decoding response body: %w", displayName, err)
		}

		data := strings.NewReader(dataJSON)
		if err = json.NewDecoder(data).Decode(&response); err != nil {
			return nil, fmt.Errorf("aws GetGroupByDisplayName: displayName: %s, error decoding response body: %w", displayName, err)
		}
	}

	return &response, nil
}

// ListGroups returns a list of groups from the AWS SSO Using the API
func (s *SCIMService) ListGroups(ctx context.Context, filter string) (*ListGroupsResponse, error) {
	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws ListGroups: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Groups")

	if filter != "" {
		q := reqURL.Query()
		q.Add("filter", filter)
		reqURL.RawQuery = q.Encode()
	}

	req, err := s.newRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aws ListGroups: error creating request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws ListGroups: error sending request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var response ListGroupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("aws ListGroups: error decoding response body: %w", err)
	}

	return &response, nil
}

// CreateGroup creates a new group in the AWS SSO Using the API
// reference:
// + https://docs.aws.amazon.com/singlesignon/latest/developerguide/creategroup.html
func (s *SCIMService) CreateGroup(ctx context.Context, g *CreateGroupRequest) (*CreateGroupResponse, error) {
	if g == nil {
		return nil, ErrCreateGroupRequestEmpty
	}
	if g.DisplayName == "" {
		return nil, ErrDisplayNameEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws CreateGroup: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Groups")

	req, err := s.newRequest(ctx, http.MethodPost, reqURL, *g)
	if err != nil {
		return nil, fmt.Errorf("aws CreateGroup: error creating request, http method: %s, url: %v, error: %w", http.MethodPost, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws CreateGroup: error sending request, http method: %s, url: %v, error: %w", http.MethodPost, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var response CreateGroupResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("aws CreateGroup: error reading response body: %w", err)
		}

		return nil, fmt.Errorf("aws CreateGroup: error decoding response body: %w, body: %s", err, string(b))
	}

	return &response, nil
}

// CreateOrGetGroup creates a new group in the AWS SSO Using the API
// This function will try to create a new group but if received a 409 http error (ConflictException	User already exists.)
// execute a request to get the group information and return it.
//
// NOTE: this function is created to avoid the existing problem with the limitation of the
// AWS SCIM API about retrieve a maximum of 50 groups at a time.
//
// references:
// + https://docs.aws.amazon.com/singlesignon/latest/developerguide/creategroup.html
// + https://docs.aws.amazon.com/singlesignon/latest/developerguide/getgroup.html
func (s *SCIMService) CreateOrGetGroup(ctx context.Context, g *CreateGroupRequest) (*CreateGroupResponse, error) {
	if g == nil {
		return nil, ErrCreateGroupRequestEmpty
	}
	if g.DisplayName == "" {
		return nil, ErrDisplayNameEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws CreateOrGetGroup: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/Groups")

	req, err := s.newRequest(ctx, http.MethodPost, reqURL, *g)
	if err != nil {
		return nil, fmt.Errorf("aws CreateOrGetGroup: error creating request, group: %s, http method: %s, url: %v, error: %w", g.DisplayName, http.MethodPost, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws CreateOrGetGroup: error sending request, group: %s, http method: %s, url: %v, error: %w", g.DisplayName, http.MethodPost, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		httpErr := new(HTTPResponseError)

		// http.StatusConflict is 409
		if errors.As(e, &httpErr) && httpErr.StatusCode == http.StatusConflict {
			log.WithFields(log.Fields{
				"name": g.DisplayName,
			}).Warn("aws CreateOrGetGroup: groups already exists, trying to get the group information")

			response, err := s.GetGroupByDisplayName(ctx, g.DisplayName)
			if err != nil {
				return nil, fmt.Errorf("aws CreateOrGetGroup: error getting group information: %w", err)
			}

			log.WithFields(log.Fields{
				"group": g.DisplayName,
				"id":    response.ID,
			}).Warn("aws CreateOrGetGroup: obtained group information")

			return &CreateGroupResponse{
				ID:          response.ID,
				Meta:        response.Meta,
				Schemas:     response.Schemas,
				DisplayName: response.DisplayName,
			}, nil
		}
		return nil, e
	}

	var response CreateGroupResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("aws CreateOrGetGroup: group: %s, error reading response body: %w", g.DisplayName, err)
		}

		return nil, fmt.Errorf("aws CreateOrGetGroup: group: %s, error decoding response body: %w, body: %s", g.DisplayName, err, string(b))
	}

	return &response, nil
}

// DeleteGroup deletes a group from the AWS SSO Using the API
func (s *SCIMService) DeleteGroup(ctx context.Context, id string) error {
	if id == "" {
		return ErrGroupIDEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return fmt.Errorf("aws DeleteGroup: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, fmt.Sprintf("/Groups/%s", id))

	req, err := s.newRequest(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("aws DeleteGroup: error creating request, http method: %s, url: %v, error: %w", http.MethodDelete, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return fmt.Errorf("aws DeleteGroup: error sending request, http method: %s, url: %v, error: %w", http.MethodDelete, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return e
	}

	return nil
}

// PatchGroup updates a group in the AWS SSO Using the API
func (s *SCIMService) PatchGroup(ctx context.Context, pgr *PatchGroupRequest) error {
	if pgr == nil {
		return ErrPatchGroupRequestEmpty
	}
	if pgr.Group.ID == "" {
		return ErrGroupIDEmpty
	}

	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return fmt.Errorf("aws PatchGroup: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, fmt.Sprintf("/Groups/%s", pgr.Group.ID))

	req, err := s.newRequest(ctx, http.MethodPatch, reqURL, pgr.Patch)
	if err != nil {
		return fmt.Errorf("aws PatchGroup: error creating request, http method: %s, url: %v, error: %w", http.MethodPatch, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return fmt.Errorf("aws PatchGroup: error sending request, http method: %s, url: %v, error: %w", http.MethodPatch, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return e
	}

	return nil
}

// ServiceProviderConfig returns additional information about the AWS SSO SCIM implementation
// references:
// + https://docs.aws.amazon.com/singlesignon/latest/developerguide/serviceproviderconfig.html
func (s *SCIMService) ServiceProviderConfig(ctx context.Context) (*ServiceProviderConfig, error) {
	reqURL, err := url.Parse(s.url.String())
	if err != nil {
		return nil, fmt.Errorf("aws ServiceProviderConfig: error parsing url: %w", err)
	}

	reqURL.Path = path.Join(reqURL.Path, "/ServiceProviderConfig")

	req, err := s.newRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aws ServiceProviderConfig: error creating request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("aws ServiceProviderConfig: error sending request, http method: %s, url: %v, error: %w", http.MethodGet, reqURL.String(), err)
	}
	defer resp.Body.Close()

	if e := s.checkHTTPResponse(resp); e != nil {
		return nil, e
	}

	var response ServiceProviderConfig
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("aws ServiceProviderConfig: error decoding response body: %w", err)
	}

	return &response, nil
}
