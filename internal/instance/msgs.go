package instance

// Msg being sent via instances edges
type Msg struct {
	Subject string
	Data    interface{}
	clb     func(data interface{}) error
}

func NewMsg(data interface{}, clb func(data interface{}) error) *Msg {
	return &Msg{Data: data, clb: clb}
}

func NewMsgWithSubject(subj string, data interface{}, clb func(data interface{}) error) *Msg {
	m := NewMsg(data, clb)
	m.Subject = subj
	return m
}

func (m *Msg) Respond(data interface{}) error {
	if m.clb == nil {
		return nil
	}
	return m.clb(data)
}
