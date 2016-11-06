package api

import (
	"github.com/golang/protobuf/proto"
	protos "github.com/pogodevorg/POGOProtos-go"
)

const hashSeed uint64 = uint64(0x61247FBF)

func protoToHash64(seed uint64, pb proto.Message) (uint64, error) {
	b, err := proto.Marshal(pb)
	if err != nil {
		return uint64(0), ErrFormatting
	}
	return Hash64Salt64(b, seed), nil
}

func protoToHash32(seed uint32, pb proto.Message) (uint32, error) {
	b, err := proto.Marshal(pb)
	if err != nil {
		return uint32(0), ErrFormatting
	}
	return Hash64Salt(b, seed), nil
}

func locationToHash32(seed uint32, location *Location) (uint32, error) {
	b := location.GetBytes()
	return Hash32Salt(b, seed), nil
}

func generateRequestHash(authTicket *protos.AuthTicket, request *protos.Request) (uint64, error) {
	h, err := protoToHash64(hashSeed, authTicket)
	if err != nil {
		return h, ErrFormatting
	}
	h, err = protoToHash64(h, request)
	if err != nil {
		return h, ErrFormatting
	}

	return h, nil
}

func generateLocation1(authTicket *protos.AuthTicket, location *Location) (uint32, error) {
	h, err := protoToHash32(uint32(hashSeed), authTicket)
	if err != nil {
		return h, ErrFormatting
	}
	h, err = locationToHash32(h, location)
	if err != nil {
		return h, ErrFormatting
	}
	return h, nil
}

func generateLocation2(location *Location) (uint32, error) {
	h, err := locationToHash32(uint32(hashSeed), location)
	if err != nil {
		return h, ErrFormatting
	}
	return h, nil
}
