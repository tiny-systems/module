package runner

import "testing"

// The transport branches on Msg.IsReplyHop to decide core-NATS addressed
// delivery vs the work-queue. Only the hop whose target IS the reply target
// diverts; every other hop of the same run rides the durable path (carrying
// the address forward).
func TestMsg_IsReplyHop(t *testing.T) {
	cases := []struct {
		name string
		msg  Msg
		want bool
	}{
		{
			name: "terminal reply hop diverts",
			msg:  Msg{To: "flow.http.srv01:response", ReplyTarget: "flow.http.srv01:response", ReplySubject: "http.reply.srv01.req9"},
			want: true,
		},
		{
			name: "mid-chain hop carries address but does not divert",
			msg:  Msg{To: "flow.common.delay01:in", ReplyTarget: "flow.http.srv01:response", ReplySubject: "http.reply.srv01.req9"},
			want: false,
		},
		{
			name: "no reply subject (background run) never diverts",
			msg:  Msg{To: "flow.http.srv01:response", ReplyTarget: "flow.http.srv01:response"},
			want: false,
		},
		{
			name: "no reply target set",
			msg:  Msg{To: "flow.http.srv01:response", ReplySubject: "http.reply.srv01.req9"},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.msg.IsReplyHop(); got != c.want {
				t.Fatalf("IsReplyHop() = %v, want %v", got, c.want)
			}
		})
	}
}
