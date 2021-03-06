package io

import (
	"github.com/OpenIoTHub/utils/models"
	"github.com/OpenIoTHub/utils/msg"
	"github.com/libp2p/go-yamux"
	"github.com/pkg/errors"
	"log"
	"time"
)

func CheckSession(muxSession *yamux.Session) error {
	//:TODO 当这个内网被删除时退出重连
	readPong := func(stream *yamux.Stream) error {
		defer stream.Close()
		var ch = make(chan struct{}, 1)
		go func() {
			_, err := msg.ReadMsg(stream)
			if err != nil {
				log.Println(err)
				return
			}
			ch <- struct{}{}
		}()
		select {
		case <-ch:
			return nil
		case <-time.After(time.Second * 3):
			if muxSession != nil && !muxSession.IsClosed() {
				muxSession.Close()
			}
			return errors.New("Session Check TimeOut")
		}
	}
	if muxSession == nil {
		return errors.New("Session is nil")
	}
	if muxSession.IsClosed() {
		return errors.New("Session.IsClosed")
	}
	stream, err := muxSession.OpenStream()
	if err != nil {
		return err
	}
	err = msg.WriteMsg(stream, &models.Ping{})
	if err != nil {
		return err
	}
	return readPong(stream)
}
