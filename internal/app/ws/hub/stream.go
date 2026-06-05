package hub

type connStream string

const (
	streamAll     connStream = "all"
	streamControl connStream = "control"
	streamMarket  connStream = "market"
)

func parseConnStream(raw string) connStream {
	switch connStream(raw) {
	case streamControl, streamMarket, streamAll:
		return connStream(raw)
	case "user":
		return streamControl
	default:
		return streamAll
	}
}

func ParseConnStream(raw string) connStream {
	return parseConnStream(raw)
}

func classifyEventStream(eventType string) connStream {
	switch eventType {
	case "time_sync", "auction_snapshot", "auction_started", "auction_extended", "auction_ended", "auction_cancelled", "user_outbid", "order_created":
		return streamControl
	default:
		return streamMarket
	}
}

func streamAccepts(conn connStream, event connStream) bool {
	return conn == streamAll || conn == event
}
