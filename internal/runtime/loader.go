package runtime

import (
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func LoadDescriptorFiles(embedded []byte) (*protoregistry.Files, error) {
	if len(embedded) == 0 {
		return nil, errors.New("embedded descriptor set is empty")
	}

	slog.Info("loading descriptor set from embedded data")

	files, err := parseDescriptorSet(embedded)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func parseDescriptorSet(raw []byte) (*protoregistry.Files, error) {
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &fds); err != nil {
		return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, fmt.Errorf("construct descriptor registry: %w", err)
	}

	return files, nil
}
