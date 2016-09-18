package ptc

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"strings"

	"net/url"

	"encoding/base64"

	"golang.org/x/net/context/ctxhttp"
)

const authorizeURL = "https://sso.pokemon.com/sso/oauth2.0/accessToken"
const loginURL = "https://sso.pokemon.com/sso/login?service=https://sso.pokemon.com/sso/oauth2.0/callbackAuthorize"

const redirectURI = "https://www.nianticlabs.com/pokemongo/error"
const clientSecret = "w8ScCUXJQc6kXKw8FiOhd8Fixzht18Dq3PEVkUCP5ZPxtgyWsbTvWHFLm2wNY0JR"
const clientID = "mobile-app_pokemon-go"

const providerString = "ptc"

type loginRequest struct {
	Lt        string   `json:"lt"`
	Execution string   `json:"execution"`
	Errors    []string `json:"errors,omitempty"`
}

// Provider contains data about and manages the session with the Pokémon Trainer's Club
type Provider struct {
	username string
	password string
	ticket   string
	http     *http.Client
}

// NewProvider constructs a Pokémon Trainer's Club auth provider instance
func NewProvider(username, password string) *Provider {
	options := &cookiejar.Options{}
	jar, _ := cookiejar.New(options)
	httpClient := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("Use the last error")
		},
	}

	return &Provider{
		http:     httpClient,
		username: username,
		password: password,
	}
}

// GetProviderString will return a string identifying the provider
func (p *Provider) GetProviderString() string {
	return providerString
}

// GetAccessToken will return an access token if it has been retrieved
func (p *Provider) GetAccessToken() string {
	return p.ticket
}

// Login retrieves an access token from the Pokémon Trainer's Club
func (p *Provider) Login(ctx context.Context, proxyId string) (string, error) {

	requestUrl := loginURL

	if proxyId != "" {
		requestUrl = "http://localhost:8080"
	}

	req1, _ := http.NewRequest("GET", requestUrl, nil)
	req1.Header.Set("User-Agent", "niantic")

	if proxyId != "" {
		req1.Header.Add("Proxy-Id", proxyId)
		req1.Header.Add("Final-Host", loginURL)
	}

	resp1, err1 := ctxhttp.Do(ctx, p.http, req1)
	if err1 != nil {
		return loginError("Could not start login process, the website might be down")
	}

	defer resp1.Body.Close()
	body1, _ := ioutil.ReadAll(resp1.Body)
	var loginRespBody loginRequest

	if proxyId != "" {
		if resp1.StatusCode == 400 {
			return "", errors.New("Dead proxy")
		}
		var proxyResponse = &ProxyResponse{}
		err := json.Unmarshal(body1, proxyResponse)
		if err != nil {
			return "", err
		}
		body1, err = base64.StdEncoding.DecodeString(proxyResponse.Response)
		if err != nil {
			return "", err
		}
	}

	json.Unmarshal(body1, &loginRespBody)
	resp1.Body.Close()

	loginForm := url.Values{}
	loginForm.Set("lt", loginRespBody.Lt)
	loginForm.Set("execution", loginRespBody.Execution)
	loginForm.Set("_eventId", "submit")
	loginForm.Set("username", p.username)
	loginForm.Set("password", p.password)

	loginFormData := strings.NewReader(loginForm.Encode())

	req2, _ := http.NewRequest("POST", requestUrl, loginFormData)
	req2.Header.Set("User-Agent", "niantic")
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if proxyId != "" {
		req2.Header.Add("Proxy-Id", proxyId)
		req2.Header.Add("Final-Host", loginURL)
	}

	resp2, err2 := ctxhttp.Do(ctx, p.http, req2)
	var proxyResponse2 = &ProxyResponse{}
	if _, ok2 := err2.(*url.Error); !ok2 {
		defer resp2.Body.Close()
		body2, _ := ioutil.ReadAll(resp2.Body)
		var respBody loginRequest
		statusCode := resp2.StatusCode
		if proxyId != "" {
			if resp2.StatusCode == 400 {
				return "", errors.New("Dead proxy")
			}
			err := json.Unmarshal(body2, proxyResponse2)
			if err != nil {
				return "", err
			}
			body2, err = base64.StdEncoding.DecodeString(proxyResponse2.Response)
			if err != nil {
				return "", err
			}
			statusCode = proxyResponse2.Status
		}

		json.Unmarshal(body2, &respBody)
		resp2.Body.Close()

		if len(respBody.Errors) > 0 {
			return loginError(respBody.Errors[0])
		}
		if statusCode != 302 {
			return loginError("Could not request authorization")
		}
	}

	var location *url.URL
	if proxyId != "" {
		location, _ = url.Parse(proxyResponse2.Location)
	} else {
		location, _ = url.Parse(resp2.Header.Get("Location"))
	}
	ticket := location.Query().Get("ticket")

	authorizeForm := url.Values{}
	authorizeForm.Set("client_id", clientID)
	authorizeForm.Set("redirect_uri", redirectURI)
	authorizeForm.Set("client_secret", clientSecret)
	authorizeForm.Set("grant_type", "refresh_token")
	authorizeForm.Set("code", ticket)

	authorizeFormData := strings.NewReader(authorizeForm.Encode())
	var req3 *http.Request
	if proxyId != "" {
		req3, _ = http.NewRequest("POST", requestUrl, authorizeFormData)
	} else {
		req3, _ = http.NewRequest("POST", authorizeURL, authorizeFormData)
	}
	req3.Header.Set("User-Agent", "niantic")
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if proxyId != "" {
		req3.Header.Add("Proxy-Id", proxyId)
		req3.Header.Add("Final-Host", authorizeURL)
	}

	resp3, err3 := ctxhttp.Do(ctx, p.http, req3)
	if err3 != nil {
		return loginError("Could not authorize code")
	}

	b, _ := ioutil.ReadAll(resp3.Body)

	if proxyId != "" {
		if resp3.StatusCode == 400 {
			return "", errors.New("Dead proxy")
		}
		var proxyResponse = &ProxyResponse{}
		err := json.Unmarshal(b, proxyResponse)
		if err != nil {
			return "", err
		}
		b, err = base64.StdEncoding.DecodeString(proxyResponse.Response)
		if err != nil {
			return "", err
		}
	}

	query, _ := url.ParseQuery(string(b))

	p.ticket = query.Get("access_token")

	return p.ticket, nil
}

type ProxyResponse struct {
	Status   int
	Response string
	Location string
}
