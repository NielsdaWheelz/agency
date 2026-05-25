package daemon

import "time"

func (s *Server) nowRFC3339() string {
	return s.clock().UTC().Format(time.RFC3339)
}
