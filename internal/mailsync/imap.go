package mailsync

import (
	"context"

	"dom-backend/internal/mailfetch"
)

// IMAP открывает папку ящика по IMAP.
type IMAP struct{ Config mailfetch.Config }

func (m IMAP) Open(ctx context.Context) (Session, error) {
	s, err := mailfetch.Open(ctx, m.Config)
	if err != nil {
		return nil, err
	}
	return s, nil
}
