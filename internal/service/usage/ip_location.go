package usage

import "strings"

// IPLocator provides a read-only IP-to-location lookup for usage records.
type IPLocator interface {
	Lookup(ip string) string
}

func (s *Service) resolveIPLocation(ip, recorded string) string {
	if strings.TrimSpace(recorded) != "" {
		return recorded
	}
	if s.location == nil || strings.TrimSpace(ip) == "" {
		return ""
	}
	return s.location.Lookup(ip)
}

func (s *Service) populateIPLocations(items []LogView) {
	locations := make(map[string]string)
	for index := range items {
		if strings.TrimSpace(items[index].IPLocation) != "" {
			continue
		}
		ip := strings.TrimSpace(items[index].ClientIP)
		if ip == "" {
			continue
		}
		location, known := locations[ip]
		if !known {
			location = s.resolveIPLocation(ip, "")
			locations[ip] = location
		}
		items[index].IPLocation = location
	}
}
