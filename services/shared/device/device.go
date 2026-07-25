package device

import (
	"strings"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

type Descriptor struct {
	ID         string
	UserAgent  string
	Browser    string
	OS         string
	FormFactor string
}

func Parse(id string, userAgent string) Descriptor {
	return Descriptor{
		ID:         strings.TrimSpace(id),
		UserAgent:  strings.TrimSpace(userAgent),
		Browser:    browser(userAgent),
		OS:         operatingSystem(userAgent),
		FormFactor: formFactor(userAgent),
	}
}

func (descriptor Descriptor) Proto() *knotv1.DeviceDescriptor {
	return &knotv1.DeviceDescriptor{
		DeviceId:   descriptor.ID,
		UserAgent:  descriptor.UserAgent,
		Browser:    descriptor.Browser,
		Os:         descriptor.OS,
		FormFactor: descriptor.FormFactor,
	}
}

func browser(value string) string {
	switch {
	case strings.Contains(value, "Edg/"):
		return "Edge"
	case strings.Contains(value, "Firefox/"):
		return "Firefox"
	case strings.Contains(value, "Chrome/") || strings.Contains(value, "CriOS/"):
		return "Chrome"
	case strings.Contains(value, "Safari/"):
		return "Safari"
	default:
		return "Unknown browser"
	}
}

func operatingSystem(value string) string {
	switch {
	case strings.Contains(value, "Android"):
		return "Android"
	case strings.Contains(value, "iPhone") || strings.Contains(value, "iPad"):
		return "iOS"
	case strings.Contains(value, "Mac OS X") || strings.Contains(value, "Macintosh"):
		return "macOS"
	case strings.Contains(value, "Windows"):
		return "Windows"
	case strings.Contains(value, "Linux"):
		return "Linux"
	default:
		return "Unknown OS"
	}
}

func formFactor(value string) string {
	switch {
	case strings.Contains(value, "iPad") || strings.Contains(value, "Tablet"):
		return "tablet"
	case strings.Contains(value, "Mobile") || strings.Contains(value, "iPhone") || strings.Contains(value, "Android"):
		return "mobile"
	default:
		return "desktop"
	}
}
