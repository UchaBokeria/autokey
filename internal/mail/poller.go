package mail

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomsg "github.com/emersion/go-message/mail"
)

// PollConfig wires the Gmail fallback poller (IMAP + app password).
type PollConfig struct {
	Username string // Gmail address (the catch-all forward destination)
	Password string // app password from secrets.env (GMAIL_APP_PASSWORD)
	Host     string // default imap.gmail.com:993
}

// PollOnce fetches UNSEEN inbox mail, stores via dedupe, marks Seen.
// Returns number of newly stored messages.
func PollOnce(ctx context.Context, sqldb *sql.DB, cfg PollConfig) (int, error) {
	host := cfg.Host
	if host == "" {
		host = "imap.gmail.com:993"
	}
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := imapclient.DialTLS(host, nil)
	if err != nil {
		return 0, fmt.Errorf("imap dial: %w", err)
	}
	defer client.Close()
	if err := client.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		return 0, fmt.Errorf("imap login: %w (need Gmail app password)", err)
	}
	defer client.Logout().Wait()
	if _, err := client.Select("INBOX", nil).Wait(); err != nil {
		return 0, fmt.Errorf("imap select: %w", err)
	}
	criteria := &imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}
	res, err := client.Search(criteria, nil).Wait()
	if err != nil {
		return 0, fmt.Errorf("imap search: %w", err)
	}
	uids, ok := res.All.(imap.UIDSet)
	if !ok {
		uids = imap.UIDSet{}
	}
	if len(uids) == 0 {
		return 0, nil
	}
	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		Flags:    true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierNone},
		},
	}
	fetchCmd := client.Fetch(uids, fetchOpts)
	defer fetchCmd.Close()
	stored := 0
	for {
		data := fetchCmd.Next()
		if data == nil {
			break
		}
		buf, err := data.Collect()
		if err != nil {
			continue
		}
		in := parseBuffer(buf)
		isNew, err := Store(dialCtx, sqldb, in)
		if err != nil {
			continue
		}
		if isNew {
			stored++
		}
		// Mark seen so next poll skips it.
		_ = client.Store(imap.UIDSetNum(buf.UID), &imap.StoreFlags{
			Op:    imap.StoreFlagsAdd,
			Flags: []imap.Flag{imap.FlagSeen},
		}, nil).Close()
	}
	return stored, nil
}

func parseBuffer(buf *imapclient.FetchMessageBuffer) Inbound {
	var in Inbound
	in.ReceivedAt = time.Now().UTC()
	if buf.Envelope != nil {
		in.Subject = buf.Envelope.Subject
		in.MessageID = buf.Envelope.MessageID
		if len(buf.Envelope.From) > 0 {
			in.Sender = buf.Envelope.From[0].Addr()
		}
		if len(buf.Envelope.To) > 0 {
			in.Recipient = buf.Envelope.To[0].Addr()
		}
	}
	var body []byte
	for _, section := range buf.BodySection {
		body = section.Bytes
		break
	}
	if len(body) == 0 {
		return in
	}
	mr, err := gomsg.CreateReader(bytes.NewReader(body))
	if err != nil {
		return in
	}
	h := mr.Header
	if in.Subject == "" {
		if s, err := h.Subject(); err == nil {
			in.Subject = s
		}
	}
	if in.MessageID == "" {
		in.MessageID, _ = h.MessageID()
	}
	if in.Recipient == "" {
		if to, err := h.AddressList("To"); err == nil && len(to) > 0 {
			in.Recipient = to[0].Address
		}
	}
	if in.Sender == "" {
		if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
			in.Sender = from[0].Address
		}
	}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if _, ok := p.Header.(*gomsg.InlineHeader); !ok {
			continue
		}
		b, _ := io.ReadAll(p.Body)
		ct := strings.ToLower(p.Header.Get("Content-Type"))
		if strings.Contains(ct, "html") {
			if in.HTML == "" {
				in.HTML = string(b)
			}
		} else if in.Text == "" {
			in.Text = string(b)
		}
	}
	return in
}
