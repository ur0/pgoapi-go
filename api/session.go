package api

import (
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"golang.org/x/net/context"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"

	"errors"

	"github.com/femot/pgoapi-go/auth"
	protos "github.com/pogodevorg/POGOProtos-go"
	mr "math/rand"
)

const defaultURL = "https://pgorelease.nianticlabs.com/plfe/rpc"
const downloadSettingsHash = "05daf51635c82611d1aac95c0b051d3ec088a930"

// Session is used to communicate with the Pokémon Go API
type Session struct {
	feed     Feed
	crypto   Crypto
	location *Location
	rpc      *RPC
	url      string
	debug    bool
	debugger *jsonpb.Marshaler

	hasTicket bool
	ticket    *protos.AuthTicket
	started   time.Time
	provider  auth.Provider
	hash      []byte
}

func generateRequests() []*protos.Request {
	return make([]*protos.Request, 0)
}

func getTimestamp(t time.Time) uint64 {
	return uint64(t.UnixNano() / int64(time.Millisecond))
}

// NewSession constructs a Pokémon Go RPC API client
func NewSession(provider auth.Provider, location *Location, feed Feed, crypto Crypto, debug bool) *Session {
	return &Session{
		location:  location,
		rpc:       NewRPC(),
		provider:  provider,
		debug:     debug,
		debugger:  &jsonpb.Marshaler{Indent: "\t"},
		feed:      feed,
		crypto:    crypto,
		started:   time.Now(),
		hasTicket: false,
		hash:      make([]byte, 32),
	}
}

// IsExpired checks the expiration timestamp of the sessions AuthTicket
// if the session has a ticket and it is still valid, the return value is false
// if there is no ticket, or the ticket is expired, the return value is true
func (s *Session) IsExpired() bool {
	if !s.hasTicket || s.ticket == nil {
		return true
	}
	return s.ticket.ExpireTimestampMs < getTimestamp(time.Now())
}

// SetTimeout sets the client timeout for the RPC API
func (s *Session) SetTimeout(d time.Duration) {
	s.rpc.http.Timeout = d
}

func (s *Session) setTicket(ticket *protos.AuthTicket) {
	s.hasTicket = true
	s.ticket = ticket
}

func (s *Session) setURL(urlToken string) {
	s.url = fmt.Sprintf("https://%s/rpc", urlToken)
}

func (s *Session) getURL() string {
	var url string
	if s.url != "" {
		url = s.url
	} else {
		url = defaultURL
	}
	return url
}

func (s *Session) debugProtoMessage(label string, pb proto.Message) {
	if s.debug {
		str, _ := s.debugger.MarshalToString(pb)
		log.Println(fmt.Sprintf("%s: %s", label, str))
	}
}

// Call queries the Pokémon Go API through RPC protobuf
func (s *Session) Call(ctx context.Context, requests []*protos.Request, proxyId int64) (*protos.ResponseEnvelope, error) {

	requestEnvelope := &protos.RequestEnvelope{
		RequestId:  uint64(8145806132888207460),
		StatusCode: int32(2),

		MsSinceLastLocationfix: int64(989),

		Longitude: s.location.Lon,
		Latitude:  s.location.Lat,

		Accuracy: int32(s.location.Accuracy),

		Requests: requests,
	}

	if s.hasTicket {
		requestEnvelope.AuthTicket = s.ticket
	} else {
		requestEnvelope.AuthInfo = &protos.RequestEnvelope_AuthInfo{
			Provider: s.provider.GetProviderString(),
			Token: &protos.RequestEnvelope_AuthInfo_JWT{
				Contents: s.provider.GetAccessToken(),
				Unknown2: int32(59),
			},
		}
	}

	if s.crypto.Enabled() && s.hasTicket {
		t := getTimestamp(time.Now())

		requestHash := make([]uint64, len(requests))

		for idx, request := range requests {
			hash, err := generateRequestHash(s.ticket, request)
			if err != nil {
				return nil, err
			}
			requestHash[idx] = hash
		}

		locationHash1, err := generateLocation1(s.ticket, s.location)
		if err != nil {
			return nil, err
		}

		locationHash2, err := generateLocation2(s.location)
		if err != nil {
			return nil, err
		}

		lf := make([]*protos.Signature_LocationFix, 1)
		lf[0] = &protos.Signature_LocationFix{
			Provider: "network",
			TimestampSnapshot: t - getTimestamp(s.started),
			Altitude: 4,
			Latitude: float32(s.location.Lat),
			Longitude: float32(s.location.Lon),
			Speed: float32(mr.Intn(15)),
			Course: float32(mr.Intn(360)),
			HorizontalAccuracy: mr.Float32(),
			VerticalAccuracy: mr.Float32(),
			ProviderStatus: 3,
			LocationType: 1,
		}

		si := make([]*protos.Signature_SensorInfo, 1)
		si[0] = &protos.Signature_SensorInfo{
			TimestampSnapshot: t - getTimestamp(s.started),
			LinearAccelerationX: mr.Float64(),
			LinearAccelerationY: mr.Float64(),
			LinearAccelerationZ: mr.Float64(),
			MagneticFieldX: mr.Float64(),
			MagneticFieldY: mr.Float64(),
			MagneticFieldZ: mr.Float64(),
			MagneticFieldAccuracy: 1,
			AttitudePitch: mr.Float64(),
			AttitudeYaw: mr.Float64(),
			// MAJOR TYPO IN PROTOS
			// Not Attitude, it's altitude
			AttitudeRoll: mr.Float64(),
			RotationRateX: mr.Float64(),
			RotationRateY: mr.Float64(),
			RotationRateZ: mr.Float64(),
			GravityX: mr.Float64(),
			GravityY: mr.Float64(),
			GravityZ: mr.Float64(),
			Status: 3,
		}

		as := &protos.Signature_ActivityStatus{
			Stationary: true,
		}

		signature := &protos.Signature{
			RequestHash:         requestHash,
			LocationHash1:       int32(locationHash1),
			LocationHash2:       int32(locationHash2),
			SessionHash:         s.hash,
			Timestamp:           t,
			TimestampSinceStart: (t - getTimestamp(s.started)),
			Unknown25:           -8408506833887075802,
			LocationFix: 				 lf,
			SensorInfo: 				 si,
			ActivityStatus: 		 as,
		}

		signatureProto, err := proto.Marshal(signature)
		if err != nil {
			return nil, ErrFormatting
		}

		iv := s.crypto.CreateIV(uint32(t - getTimestamp(s.started)))
		encryptedSignature, err := s.crypto.Encrypt(signatureProto, iv)
		if err != nil {
			return nil, ErrFormatting
		}

		requestMessage, err := proto.Marshal(&protos.SendEncryptedSignatureRequest{
			EncryptedSignature: encryptedSignature,
		})
		if err != nil {
			return nil, ErrFormatting
		}

		requestEnvelope.PlatformRequests = []*protos.RequestEnvelope_PlatformRequest{
			{
				Type:           protos.PlatformRequestType_SEND_ENCRYPTED_SIGNATURE,
				RequestMessage: requestMessage,
			},
		}

		s.debugProtoMessage("request signature", signature)
	}

	s.debugProtoMessage("request envelope", requestEnvelope)

	responseEnvelope, err := s.rpc.Request(ctx, s.getURL(), requestEnvelope, proxyId)

	s.debugProtoMessage("response envelope", responseEnvelope)

	return responseEnvelope, err
}

// MoveTo sets your current location
func (s *Session) MoveTo(location *Location) {
	s.location = location
}

// Init initializes the client by performing full authentication
func (s *Session) Init(ctx context.Context, proxyId int64) error {
	_, err := s.provider.Login(ctx)
	if err != nil {
		return err
	}

	_, err = rand.Read(s.hash)
	if err != nil {
		return ErrFormatting
	}

	settingsMessage, _ := proto.Marshal(&protos.DownloadSettingsMessage{
		Hash: downloadSettingsHash,
	})

	requests := []*protos.Request{
		{RequestType: protos.RequestType_GET_PLAYER},
		{RequestType: protos.RequestType_GET_HATCHED_EGGS},
		{RequestType: protos.RequestType_GET_INVENTORY},
		{RequestType: protos.RequestType_CHECK_AWARDED_BADGES},
		{protos.RequestType_DOWNLOAD_SETTINGS, settingsMessage},
	}

	response, err := s.Call(ctx, requests, proxyId)
	if err != nil {
		return err
	}

	url := response.ApiUrl
	if url == "" {
		return ErrNoURL
	}
	s.setURL(url)

	ticket := response.GetAuthTicket()
	s.setTicket(ticket)

	return nil
}

// Announce publishes the player's presence and returns the map environment
func (s *Session) Announce(ctx context.Context, proxyId int64) (mapObjects *protos.GetMapObjectsResponse, err error) {

	cellIDs := s.location.GetCellIDs()
	lastTimestamp := time.Now().Unix() * 1000

	settingsMessage, _ := proto.Marshal(&protos.DownloadSettingsMessage{
		Hash: downloadSettingsHash,
	})
	// Request the map objects based on my current location and route cell ids
	getMapObjectsMessage, _ := proto.Marshal(&protos.GetMapObjectsMessage{
		// Traversed route since last supposed last heartbeat
		CellId: cellIDs,

		// Timestamps in milliseconds corresponding to each route cell id
		SinceTimestampMs: make([]int64, len(cellIDs)),

		// Current longitide and latitude
		Longitude: s.location.Lon,
		Latitude:  s.location.Lat,
	})
	// Request the inventory with a message containing the current time
	getInventoryMessage, _ := proto.Marshal(&protos.GetInventoryMessage{
		LastTimestampMs: lastTimestamp,
	})
	requests := []*protos.Request{
		{RequestType: protos.RequestType_GET_PLAYER},
		{RequestType: protos.RequestType_GET_HATCHED_EGGS},
		{protos.RequestType_GET_INVENTORY, getInventoryMessage},
		{RequestType: protos.RequestType_CHECK_AWARDED_BADGES},
		{protos.RequestType_DOWNLOAD_SETTINGS, settingsMessage},
		{protos.RequestType_GET_MAP_OBJECTS, getMapObjectsMessage},
		{RequestType: protos.RequestType_CHECK_CHALLENGE},
	}

	response, err := s.Call(ctx, requests, proxyId)
	if err != nil {
		if err == ErrProxyDead {
			return mapObjects, err
		}
		return mapObjects, ErrRequest
	}

	mapObjects = &protos.GetMapObjectsResponse{}
	if len(response.Returns) < 5 {
		return nil, errors.New("Empty response")
	}
	err = proto.Unmarshal(response.Returns[5], mapObjects)
	if err != nil {
		return nil, &ErrResponse{err}
	}
	s.feed.Push(mapObjects)
	s.debugProtoMessage("response return[5]", mapObjects)

	challenge := protos.CheckChallengeResponse{}
	err = proto.Unmarshal(response.Returns[6], &challenge)
	if challenge.ShowChallenge {
		return mapObjects, ErrCheckChallenge
	}

	return mapObjects, GetErrorFromStatus(response.StatusCode)
}

// GetPlayer returns the current player profile
func (s *Session) GetPlayer(ctx context.Context, proxyId int64) (*protos.GetPlayerResponse, error) {
	requests := []*protos.Request{{RequestType: protos.RequestType_GET_PLAYER}}
	response, err := s.Call(ctx, requests, proxyId)
	if err != nil {
		return nil, err
	}

	player := &protos.GetPlayerResponse{}
	err = proto.Unmarshal(response.Returns[0], player)
	if err != nil {
		return nil, &ErrResponse{err}
	}
	s.feed.Push(player)
	s.debugProtoMessage("response return[0]", player)

	return player, GetErrorFromStatus(response.StatusCode)
}

// GetPlayerMap returns the surrounding map cells
func (s *Session) GetPlayerMap(ctx context.Context, proxyId int64) (*protos.GetMapObjectsResponse, error) {
	return s.Announce(ctx, proxyId)
}

// GetInventory returns the player items
func (s *Session) GetInventory(ctx context.Context, proxyId int64) (*protos.GetInventoryResponse, error) {
	requests := []*protos.Request{{RequestType: protos.RequestType_GET_INVENTORY}}
	response, err := s.Call(ctx, requests, proxyId)
	if err != nil {
		return nil, err
	}
	inventory := &protos.GetInventoryResponse{}
	err = proto.Unmarshal(response.Returns[0], inventory)
	if err != nil {
		return nil, &ErrResponse{err}
	}
	s.feed.Push(inventory)
	s.debugProtoMessage("response return[0]", inventory)

	return inventory, GetErrorFromStatus(response.StatusCode)
}
