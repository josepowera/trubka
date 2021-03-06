package protobuf

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/dynamic"

	"github.com/xitonix/trubka/internal"
)

// Loader the interface to load and list the protocol buffer message types.
type Loader interface {
	Load(ctx context.Context, messageName string) error
	Get(messageName string) (*dynamic.Message, error)
	List(filter *regexp.Regexp) ([]string, error)
}

// FileLoader is an implementation of Loader interface to load the proto files from the disk.
type FileLoader struct {
	files   []*desc.FileDescriptor
	cache   map[string]*desc.MessageDescriptor
	factory *dynamic.MessageFactory
	root    string
}

// LoadFiles creates a new instance of local file loader.
func LoadFiles(ctx context.Context, verbosity internal.VerbosityLevel, root string) (*FileLoader, error) {
	finder, err := newFileFinder(verbosity, root)
	if err != nil {
		return nil, err
	}

	files, err := finder.ls(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load the proto files: %w", err)
	}

	importPaths, err := finder.dirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load the import paths: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no protocol buffer (*.proto) files found in %s", root)
	}
	resolved, err := protoparse.ResolveFilenames(importPaths, files...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the protocol buffer (*.proto) files: %w", err)
	}

	parser := protoparse.Parser{
		ImportPaths:           importPaths,
		IncludeSourceCodeInfo: true,
	}

	fileDescriptors, err := parser.ParseFiles(resolved...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the protocol buffer (*.proto) files: %w", err)
	}

	er := &dynamic.ExtensionRegistry{}
	for _, fd := range fileDescriptors {
		er.AddExtensionsFromFile(fd)
	}

	return &FileLoader{
		files:   fileDescriptors,
		cache:   make(map[string]*desc.MessageDescriptor),
		factory: dynamic.NewMessageFactoryWithExtensionRegistry(er),
		root:    root,
	}, nil
}

// Load loads the specified message type into the local cache.
//
// The input parameter must be the fully qualified name of the message type.
// The method will return an error if the specified message type does not exist in the path.
//
// Calling load is not thread safe.
func (f *FileLoader) Load(ctx context.Context, messageName string) error {
	_, ok := f.cache[messageName]
	if ok {
		return nil
	}
	for _, fd := range f.files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			md := fd.FindMessage(messageName)
			if md != nil {
				f.cache[messageName] = md
				return nil
			}
		}
	}
	return fmt.Errorf("%s has not been found in %s", messageName, f.root)
}

// Get creates a new instance of the specified protocol buffer message.
//
// The input parameter must be the fully qualified name of the message type.
// The method will return an error if the specified message type does not exist in the path.
func (f *FileLoader) Get(messageName string) (*dynamic.Message, error) {
	if md, ok := f.cache[messageName]; ok {
		return f.factory.NewDynamicMessage(md), nil
	}
	return nil, fmt.Errorf("%s has not been found in %s. Make sure you Load the message first", messageName, f.root)
}

// List returns a list of all the protocol buffer messages exist in the path.
func (f *FileLoader) List(search *regexp.Regexp) ([]string, error) {
	result := make([]string, 0)
	for _, fd := range f.files {
		messages := fd.GetMessageTypes()
		for _, msg := range messages {
			name := msg.GetFullyQualifiedName()
			if search == nil {
				result = append(result, name)
				continue
			}
			if search.Match([]byte(name)) {
				result = append(result, name)
			}
		}
	}
	return result, nil
}
