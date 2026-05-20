// Copyright 2026 The antigravity-sdk-go Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agtypes

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// FileChange is a single filesystem change detected by a file-watching trigger.
// Treat as immutable.
type FileChange struct {
	// Kind is the type of change (added, modified, deleted).
	Kind FileChangeKind `json:"kind"`
	// Path is the absolute path to the changed file.
	Path string `json:"path"`
}

// Supported MIME types per media category. These mirror the upstream frozensets.
var (
	supportedImageMIMEs = map[string]struct{}{
		"image/bmp":  {},
		"image/jpeg": {},
		"image/png":  {},
		"image/webp": {},
	}
	supportedDocumentMIMEs = map[string]struct{}{
		"application/pdf":  {},
		"application/json": {},
		"text/css":         {},
		"text/csv":         {},
		"text/html":        {},
		"text/javascript":  {},
		"text/plain":       {},
		"text/rtf":         {},
		"text/xml":         {},
	}
	supportedAudioMIMEs = map[string]struct{}{
		"audio/wav":  {},
		"audio/mp3":  {},
		"audio/aac":  {},
		"audio/ogg":  {},
		"audio/flac": {},
		"audio/opus": {},
		"audio/mpeg": {},
		"audio/m4a":  {},
		"audio/l16":  {},
	}
	supportedVideoMIMEs = map[string]struct{}{
		"video/3gpp":      {},
		"video/avi":       {},
		"video/mp4":       {},
		"video/mpeg":      {},
		"video/mpg":       {},
		"video/quicktime": {},
		"video/webm":      {},
		"video/wmv":       {},
		"video/x-flv":     {},
	}
)

// Media is the interface implemented by all rich multimedia content attachment
// primitives (Image, Document, Audio, Video). Implementations are closed to
// this package via the unexported marker method. Treat values as immutable.
type Media interface {
	isMedia()
	// Bytes returns the raw media data.
	Bytes() []byte
	// MIME returns the media MIME type.
	MIME() string
	// Desc returns the optional human-readable description.
	Desc() string
}

// baseMedia is the common fields for every media primitive.
type baseMedia struct {
	// Data is the raw media bytes.
	Data []byte `json:"data"`
	// MIMEType is the media MIME type.
	MIMEType string `json:"mime_type"`
	// Description is an optional human-readable description.
	Description string `json:"description,omitzero"`
}

func (m baseMedia) isMedia()      {}
func (m baseMedia) Bytes() []byte { return m.Data }
func (m baseMedia) MIME() string  { return m.MIMEType }
func (m baseMedia) Desc() string  { return m.Description }

// Image is an image content attachment primitive.
type Image struct{ baseMedia }

// Document is a document content attachment primitive.
type Document struct{ baseMedia }

// Audio is an audio content attachment primitive.
type Audio struct{ baseMedia }

// Video is a video content attachment primitive.
type Video struct{ baseMedia }

// NewImage returns an Image, validating that mimeType is a supported image MIME
// type.
func NewImage(data []byte, mimeType, description string) (Image, error) {
	if _, ok := supportedImageMIMEs[mimeType]; !ok {
		return Image{}, fmt.Errorf("agtypes: unsupported Image MIME type: %q", mimeType)
	}
	return Image{baseMedia{Data: data, MIMEType: mimeType, Description: description}}, nil
}

// NewDocument returns a Document, validating that mimeType is a supported
// document MIME type.
func NewDocument(data []byte, mimeType, description string) (Document, error) {
	if _, ok := supportedDocumentMIMEs[mimeType]; !ok {
		return Document{}, fmt.Errorf("agtypes: unsupported Document MIME type: %q", mimeType)
	}
	return Document{baseMedia{Data: data, MIMEType: mimeType, Description: description}}, nil
}

// NewAudio returns an Audio, validating that mimeType is a supported audio MIME
// type.
func NewAudio(data []byte, mimeType, description string) (Audio, error) {
	if _, ok := supportedAudioMIMEs[mimeType]; !ok {
		return Audio{}, fmt.Errorf("agtypes: unsupported Audio MIME type: %q", mimeType)
	}
	return Audio{baseMedia{Data: data, MIMEType: mimeType, Description: description}}, nil
}

// NewVideo returns a Video, validating that mimeType is a supported video MIME
// type.
func NewVideo(data []byte, mimeType, description string) (Video, error) {
	if _, ok := supportedVideoMIMEs[mimeType]; !ok {
		return Video{}, fmt.Errorf("agtypes: unsupported Video MIME type: %q", mimeType)
	}
	return Video{baseMedia{Data: data, MIMEType: mimeType, Description: description}}, nil
}

// mimeToConstructor maps each supported MIME type to a constructor, built once
// from the per-category sets. It backs FromFile's MIME dispatch.
var mimeToConstructor = func() map[string]func([]byte, string, string) (Media, error) {
	m := make(map[string]func([]byte, string, string) (Media, error))
	for mt := range supportedImageMIMEs {
		m[mt] = func(d []byte, mt, desc string) (Media, error) { return NewImage(d, mt, desc) }
	}
	for mt := range supportedDocumentMIMEs {
		m[mt] = func(d []byte, mt, desc string) (Media, error) { return NewDocument(d, mt, desc) }
	}
	for mt := range supportedAudioMIMEs {
		m[mt] = func(d []byte, mt, desc string) (Media, error) { return NewAudio(d, mt, desc) }
	}
	for mt := range supportedVideoMIMEs {
		m[mt] = func(d []byte, mt, desc string) (Media, error) { return NewVideo(d, mt, desc) }
	}
	return m
}()

// FromFile resolves a local file path into the correct Media primitive (Image,
// Document, Audio, or Video) based on the file's MIME type, inferred from its
// extension.
//
// It returns an error if the file cannot be read, the MIME type cannot be
// inferred, or the inferred type is unsupported.
func FromFile(path, description string) (Media, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agtypes: read file %q: %w", path, err)
	}
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	// Strip any parameters (e.g. "; charset=utf-8") to match the registry keys.
	if base, _, found := strings.Cut(mimeType, ";"); found {
		mimeType = strings.TrimSpace(base)
	}
	if mimeType == "" {
		return nil, fmt.Errorf("agtypes: could not infer a valid MIME type for extension: %q", filepath.Ext(path))
	}
	ctor, ok := mimeToConstructor[mimeType]
	if !ok {
		return nil, fmt.Errorf("agtypes: unsupported MIME type: %q; supported formats: %v", mimeType, supportedMIMEs())
	}
	return ctor(data, mimeType, description)
}

// supportedMIMEs returns the sorted list of all supported MIME types, for error
// messages.
func supportedMIMEs() []string {
	out := make([]string, 0, len(mimeToConstructor))
	for mt := range mimeToConstructor {
		out = append(out, mt)
	}
	slices.Sort(out)
	return out
}
