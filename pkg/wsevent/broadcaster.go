package wsevent

type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Broadcaster interface {
	Fanout(topic string, event Event) error
	Unicast(addr string, event Event) error
}

func RoomTopic(roomID string) string { return "room:" + roomID }
func UserAddr(userID string) string  { return "user:" + userID }
