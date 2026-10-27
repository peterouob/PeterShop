package etcdtest

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

func StartETCDEmbed() (*clientv3.Client, func(), error) {
	dir, err := os.MkdirTemp("", "etcdtest-*")
	if err != nil {
		return nil, nil, err
	}

	clientURL, err := freeURL()
	if err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return nil, nil, err
		}
		return nil, nil, err
	}

	peerURL, err := freeURL()
	if err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return nil, nil, err
		}
		return nil, nil, err
	}

	cfg := embed.NewConfig()
	cfg.Dir = dir
	cfg.LogLevel = "error"
	cfg.ListenClientUrls = []url.URL{clientURL}
	cfg.AdvertiseClientUrls = []url.URL{clientURL}
	cfg.ListenPeerUrls = []url.URL{peerURL}
	cfg.AdvertisePeerUrls = []url.URL{peerURL}
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)

	cfg.TickMs = 10
	cfg.ElectionMs = 100

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return nil, nil, err
		}
		return nil, nil, err
	}

	select {
	case <-e.Server.ReadyNotify():
	case err = <-e.Err():
		e.Close()
		if err := os.RemoveAll(dir); err != nil {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("etcdtest: start: %w", err)
	case <-time.After(10 * time.Second):
		e.Server.Stop()
		e.Close()
		if err := os.RemoveAll(dir); err != nil {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("etcdtest: start timeout")
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{clientURL.Host},
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		e.Close()
		if err := os.RemoveAll(dir); err != nil {
			return nil, nil, err
		}
		return nil, nil, err
	}

	stop := func() {
		if err := cli.Close(); err != nil {
			return
		}
		if err != nil {
			return
		}
		e.Close()
		if err := os.RemoveAll(dir); err != nil {
			return
		}
	}
	return cli, stop, nil
}

func freeURL() (url.URL, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return url.URL{}, err
	}
	defer func(l net.Listener) {
		if err := l.Close(); err != nil {
			return
		}
	}(l)

	return url.URL{Scheme: "http", Host: l.Addr().String()}, nil
}
