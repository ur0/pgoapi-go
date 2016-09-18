package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"

	"golang.org/x/net/context/ctxhttp"

	"github.com/golang/protobuf/proto"
	protos "github.com/pogodevorg/POGOProtos-go"
)

const rpcUserAgent = "Niantic App"

var ProxyHost = ""

func raise(message string) error {
	return fmt.Errorf("rpc/client: %s", message)
}

// RPC is used to communicate with the Pokémon Go API
type RPC struct {
	http *http.Client
}

// NewRPC constructs a Pokémon Go RPC API client
func NewRPC() *RPC {
	options := &cookiejar.Options{}
	jar, _ := cookiejar.New(options)
	httpClient := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return raise("Did not follow redirect")
		},
	}

	return &RPC{
		http: httpClient,
	}
}

// Request queries the Pokémon Go API will all pending requests
func (c *RPC) Request(ctx context.Context, endpoint string, requestEnvelope *protos.RequestEnvelope, proxyId string) (responseEnvelope *protos.ResponseEnvelope, err error) {
	responseEnvelope = &protos.ResponseEnvelope{}

	// Build request
	requestBytes, err := proto.Marshal(requestEnvelope)
	if err != nil {
		return responseEnvelope, raise("Could not encode request body")
	}
	requestReader := bytes.NewReader(requestBytes)

	// Create request
	var request *http.Request
	if proxyId != "" {
		request, err = http.NewRequest("POST", ProxyHost, requestReader)
		request.Header.Add("Proxy-Id", proxyId)
		request.Header.Add("Final-Host", endpoint)
		request.Host = ProxyHost
	} else {
		request, err = http.NewRequest("POST", endpoint, requestReader)
	}
	if err != nil {
		return responseEnvelope, raise("Unable to create the request")
	}
	request.Header.Add("User-Agent", rpcUserAgent)

	// Perform call to API
	response, err := ctxhttp.Do(ctx, c.http, request)
	if err != nil {
		return responseEnvelope, raise(fmt.Sprintf("There was an error requesting the API: %s", err))
	}
	defer response.Body.Close()

	if response.StatusCode == 400 {
		return responseEnvelope, ErrProxyDead
	}

	if response.StatusCode != 200 {
		return responseEnvelope, raise(fmt.Sprintf("Status code was %d, expected 200", response.StatusCode))
	}

	// Read the response
	responseBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return responseEnvelope, raise("Could not decode response body")
	}

	if proxyId != "" {
		var proxyResponse = &ProxyResponse{}
		err = json.Unmarshal(responseBytes, proxyResponse)
		if err != nil {
			return responseEnvelope, raise("Could not decode response body")
		}
		if proxyResponse.Status != 200 {
			return responseEnvelope, raise(fmt.Sprintf("Status code was %d, expected 200", proxyResponse.Status))
		}

		decoded, err := base64.StdEncoding.DecodeString(proxyResponse.Response)
		if err != nil {
			return responseEnvelope, err
		}

		proto.Unmarshal(decoded, responseEnvelope)
	} else {
		proto.Unmarshal(responseBytes, responseEnvelope)
	}
	if responseEnvelope.StatusCode != protos.ResponseEnvelope_OK && responseEnvelope.StatusCode != protos.ResponseEnvelope_OK_RPC_URL_IN_RESPONSE && responseEnvelope.StatusCode != protos.ResponseEnvelope_REDIRECT {
		return responseEnvelope, GetErrorFromStatus(responseEnvelope.StatusCode)
	}
	return responseEnvelope, nil
}

type ProxyResponse struct {
	Status   int
	Response string
	Headers  map[string]string
}
