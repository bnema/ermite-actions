// SPDX-License-Identifier: MIT

package action

import (
	"fmt"
	"strings"
)

// UsesKind says where the action named by a "uses:" value lives.
type UsesKind int

// The kinds of "uses:" value.
const (
	// UsesDocker is "docker://image[:tag]".
	UsesDocker UsesKind = iota + 1
	// UsesLocal is "./path" or ".\path", relative to the workspace.
	UsesLocal
	// UsesRepository is "owner/repo[/path]@ref".
	UsesRepository
	// UsesSelfRepository is "$/[path]": an action in the repository of the
	// workflow or action that contains the step.
	UsesSelfRepository
)

// Uses is a parsed "uses:" value of a step.
type Uses struct {
	Kind UsesKind
	// Image is the image of a UsesDocker value, without "docker://".
	Image string
	// Path is the value itself for UsesLocal ("./x"), the directory inside
	// the repository for UsesRepository ("" for the repository's root, else
	// "a/b" with / as separator), and the directory after "$/" for
	// UsesSelfRepository.
	Path string
	// Owner and Repo name the repository of a UsesRepository value, and Name is
	// "Owner/Repo". Ref is its ref: a branch, a tag or a commit sha.
	Owner, Repo, Name, Ref string
}

// UsesError is the error of [ParseUses].
type UsesError struct {
	// Value is the "uses:" value that was refused.
	Value string
	// Message is the text GitHub's runner reports, without a position.
	Message string
}

func (e *UsesError) Error() string { return e.Message }

// ParseUses parses the "uses:" value of a step. It is the runner's check
// (WorkflowTemplateConverter.ConvertToStep and the legacy
// PipelineTemplateConverter.ConvertToStep, which agree) and returns its
// messages:
//
//   - "Expected format {org}/{repo}[/path]@ref. Actual 'x'" for a value that is
//     not docker://, ./ or .\ or $/ and does not have exactly one "@", a
//     non-empty ref, and an owner and a repository before it. Path separators
//     are / and \, and empty segments are ignored ("a//b@c" is a/b@c, and
//     "/a/b@c" too).
//   - "Self-repository references do not support an '@ref' suffix. Actual
//     '$/x@v1'" for a $/ value with an "@".
//
// The prefixes docker:// are compared as written (case-sensitive), as the
// converter does. The language service words the first message differently
// ("{owner}/{repo}[/path]@{ref}") and refuses a blank value with another
// text; PORTING.md lists the differences. Whether the repository exists, or
// whether the ref does, is for whoever resolves the action to say.
func ParseUses(s string) (Uses, error) {
	switch {
	case strings.HasPrefix(s, "docker://"):
		return Uses{Kind: UsesDocker, Image: s[len("docker://"):]}, nil
	case strings.HasPrefix(s, "./"), strings.HasPrefix(s, `.\`):
		return Uses{Kind: UsesLocal, Path: s}, nil
	case strings.HasPrefix(s, "$/"):
		path := strings.TrimLeft(s[2:], "/")
		if strings.Contains(path, "@") {
			return Uses{}, &UsesError{Value: s, Message: fmt.Sprintf("Self-repository references do not support an '@ref' suffix. Actual '%s'", s)}
		}
		return Uses{Kind: UsesSelfRepository, Path: path}, nil
	}
	segments := strings.Split(s, "@")
	path := strings.FieldsFunc(segments[0], func(r rune) bool { return r == '/' || r == '\\' })
	if len(segments) != 2 || len(path) < 2 || segments[1] == "" {
		return Uses{}, &UsesError{Value: s, Message: fmt.Sprintf("Expected format {org}/{repo}[/path]@ref. Actual '%s'", s)}
	}
	return Uses{
		Kind:  UsesRepository,
		Owner: path[0],
		Repo:  path[1],
		Name:  path[0] + "/" + path[1],
		Path:  strings.Join(path[2:], "/"),
		Ref:   segments[1],
	}, nil
}

// ImageKind says what a docker action's "image" is.
type ImageKind int

// The kinds of "runs.image".
const (
	// ImageDockerfile is a path to a Dockerfile in the action's directory.
	ImageDockerfile ImageKind = iota + 1
	// ImageRegistry is "docker://image[:tag]": an image to pull.
	ImageRegistry
)

// Image is a parsed "runs.image" of a docker action.
type Image struct {
	Kind ImageKind
	// Path is the Dockerfile's path relative to the action's directory, for
	// ImageDockerfile.
	Path string
	// Ref is the image to pull, without "docker://", for ImageRegistry.
	Ref string
}

// ParseImage classifies the "image" of a docker action as the runner does
// (ActionManager.PrepareRepositoryActionAsync and DockerUtil.IsDockerfile):
// a path whose last segment starts with "Dockerfile." or ends with
// "Dockerfile" (case-insensitively) is a Dockerfile, "docker://" (any case)
// followed by an image is an image to pull, and anything else fails with
// "'x' should be either '[path]/Dockerfile' or 'docker://image[:tag]'.".
// [Parse] does not call it: the runner checks the image when it prepares the
// action, after loading the manifest.
func ParseImage(image string) (Image, error) {
	if hasPrefixFold(image, "docker://") {
		return Image{Kind: ImageRegistry, Ref: image[len("docker://"):]}, nil
	}
	last := image[strings.LastIndex(image, "/")+1:]
	if hasPrefixFold(last, "Dockerfile.") || hasSuffixFold(last, "Dockerfile") {
		return Image{Kind: ImageDockerfile, Path: image}, nil
	}
	return Image{}, &UsesError{Value: image, Message: fmt.Sprintf("'%s' should be either '[path]/Dockerfile' or 'docker://image[:tag]'.", image)}
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && asciiEqualFold(s[:len(prefix)], prefix)
}

func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && asciiEqualFold(s[len(s)-len(suffix):], suffix)
}
