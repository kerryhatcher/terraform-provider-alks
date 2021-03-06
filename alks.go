package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/terraform/helper/schema"
)

type AlksAccount struct {
	Username string `json:"userid"`
	Password string `json:"password"`
	Account  string `json:"account"`
	Role     string `json:"role"`
}

type AlksClient struct {
	Account AlksAccount
	BaseURL string

	Http *http.Client
}

type CreateIamKeyReq struct {
	SessionTime int `json:"sessionTime"`
}

type CreateIamRoleReq struct {
	RoleName   string `json:"roleName"`
	RoleType   string `json:"roleType"`
	IncDefPols int    `json:"includeDefaultPolicy"`
}

type StsResponse struct {
	AccessKey    string `json:"accessKey"`
	SessionKey   string `json:"secretKey"`
	SessionToken string `json:"sessionToken"`
}

type CreateRoleResponse struct {
	RoleName      string   `json:"roleName"`
	RoleType      string   `json:"roleType"`
	RoleArn       string   `json:"roleArn"`
	RoleIPArn     string   `json:"instanceProfileArn"`
	RoleAddedToIP bool     `json:"addedRoleToInstanceProfile"`
	Errors        []string `json:"errors"`
}

type GetRoleRequest struct {
	RoleName string `json:"roleName"`
}

type GetRoleResponse struct {
	RoleName  string   `json:"roleName"`
	RoleArn   string   `json:"roleArn"`
	RoleIPArn string   `json:"instanceProfileArn"`
	Exists    bool     `json:"roleExists"`
	Errors    []string `json:"errors"`
}

type DeleteRoleRequest struct {
	RoleName string `json:"roleName"`
}

type DeleteRoleResponse struct {
	RoleName string   `json:"roleName"`
	Status   string   `json:"roleArn"`
	Errors   []string `json:"errors"`
}

func NewAlksClient(url string, username string, password string, account string, role string) (*AlksClient, error) {
	alksClient := AlksClient{
		Account: AlksAccount{
			Username: username,
			Password: password,
			Account:  account,
			Role:     role,
		},
		BaseURL: url,
		Http:    cleanhttp.DefaultClient(),
	}

	return &alksClient, nil
}

func (c *AlksClient) NewRequest(json []byte, method string, endpoint string) (*http.Request, error) {
	u, err := url.Parse(c.BaseURL + endpoint)

	if err != nil {
		return nil, fmt.Errorf("Error parsing base URL: %s", err)
	}

	req, err := http.NewRequest(method, u.String(), bytes.NewBuffer(json))

	if err != nil {
		return nil, fmt.Errorf("Error creating request: %s", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func decodeBody(resp *http.Response, out interface{}) error {
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return err
	}

	if err = json.Unmarshal(body, &out); err != nil {
		return err
	}

	return nil
}

func checkResp(resp *http.Response, err error) (*http.Response, error) {
	if err != nil {
		return resp, err
	}

	switch i := resp.StatusCode; {
	case i == 200:
		return resp, nil
	case i == 201:
		return resp, nil
	case i == 202:
		return resp, nil
	case i == 204:
		return resp, nil
	case i == 400:
		return nil, fmt.Errorf("API Error 400: %s", resp.Status)
	case i == 401:
		return nil, fmt.Errorf("API Error 401: %s", resp.Status)
	case i == 402:
		return nil, fmt.Errorf("API Error 402: %s", resp.Status)
	case i == 422:
		return nil, fmt.Errorf("API Error 422: %s", resp.Status)
	default:
		return nil, fmt.Errorf("API Error: %s", resp.Status)
	}
}

func (c *AlksClient) CreateIamKey() (*StsResponse, error) {

	iam := CreateIamKeyReq{1}
	b, err := json.Marshal(struct {
		CreateIamKeyReq
		AlksAccount
	}{iam, c.Account})

	if err != nil {
		return nil, fmt.Errorf("Error encoding IAM create key JSON: %s", err)
	}

	req, err := c.NewRequest(b, "POST", "/getIAMKeys/")
	if err != nil {
		return nil, err
	}

	resp, err := checkResp(c.Http.Do(req))
	if err != nil {
		return nil, err
	}

	sts := new(StsResponse)
	err = decodeBody(resp, &sts)

	if err != nil {
		return nil, fmt.Errorf("Error parsing STS response: %s", err)
	}

	return sts, nil
}

func (c *AlksClient) CreateIamRole(roleName string, roleType string, includeDefaultPolicies bool) (*CreateRoleResponse, error) {
	var include int = 0
	if includeDefaultPolicies {
		include = 1
	}

	iam := CreateIamRoleReq{
		roleName,
		roleType,
		include,
	}

	b, err := json.Marshal(struct {
		CreateIamRoleReq
		AlksAccount
	}{iam, c.Account})

	if err != nil {
		return nil, fmt.Errorf("Error encoding IAM create role JSON: %s", err)
	}

	req, err := c.NewRequest(b, "POST", "/createRole/")
	if err != nil {
		return nil, err
	}

	resp, err := checkResp(c.Http.Do(req))
	if err != nil {
		return nil, err
	}

	cr := new(CreateRoleResponse)
	err = decodeBody(resp, &cr)

	if err != nil {
		return nil, fmt.Errorf("Error parsing CreateRole response: %s", err)
	}

	if len(cr.Errors) > 0 {
		return nil, fmt.Errorf("Error creating role: %s", strings.Join(cr.Errors[:], ", "))
	}

	return cr, nil
}

func (c *AlksClient) DeleteIamRole(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Deleting IAM role: %s", d.Id())

	rmRole := DeleteRoleRequest{d.Id()}

	b, err := json.Marshal(struct {
		DeleteRoleRequest
		AlksAccount
	}{rmRole, c.Account})

	if err != nil {
		return fmt.Errorf("Error encoding IAM delete role JSON: %s", err)
	}

	req, err := c.NewRequest(b, "POST", "/deleteRole/")
	if err != nil {
		return err
	}

	resp, err := checkResp(c.Http.Do(req))
	if err != nil {
		return err
	}

	del := new(DeleteRoleResponse)
	err = decodeBody(resp, &del)

	if err != nil {
		return fmt.Errorf("Error parsing DeleteRole response: %s", err)
	}

	// TODO you get an error if you delete an already deleted role, need to revist for checking fail/success
	if len(del.Errors) > 0 {
		return fmt.Errorf("Error deleting role: %s", strings.Join(del.Errors[:], ", "))
	}

	return nil
}

func (c *AlksClient) GetIamRole(roleName string) (*GetRoleResponse, error) {
	log.Printf("[INFO] Getting IAM role: %s", roleName)
	getRole := GetRoleRequest{roleName}

	b, err := json.Marshal(struct {
		GetRoleRequest
		AlksAccount
	}{getRole, c.Account})

	if err != nil {
		return nil, fmt.Errorf("Error encoding IAM create role JSON: %s", err)
	}

	req, err := c.NewRequest(b, "POST", "/getAccountRole/")
	if err != nil {
		return nil, err
	}

	resp, err := checkResp(c.Http.Do(req))
	if err != nil {
		return nil, err
	}

	cr := new(GetRoleResponse)
	err = decodeBody(resp, &cr)

	if err != nil {
		return nil, fmt.Errorf("Error parsing GetRole response: %s", err)
	}

	if len(cr.Errors) > 0 {
		return nil, fmt.Errorf("Error getting role: %s", strings.Join(cr.Errors[:], ", "))
	}

	if !cr.Exists {
		return nil, nil
	}

	return cr, nil
}
