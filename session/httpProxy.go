package session

import (
	"context"
	"fmt"
	"github.com/OpenIoTHub/server-grpc-api/pb-go"
	"github.com/OpenIoTHub/utils/io"
	"github.com/libp2p/go-yamux"
	"golang.org/x/net/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

//服务结构
type HttpProxy struct {
	Domain           string
	RunId            string
	RemoteIP         string
	RemotePort       int
	UserName         string
	Password         string
	IfHttps          bool
	Description      string
	RemotePortStatus bool
}

func (hp *HttpProxy) UpdateRemotePortStatus() {
	var online bool
	if hp.IfHttps {
		online, _ = SessionsCtl.CheckRemoteStatus("tls", hp.RunId, hp.RemoteIP, hp.RemotePort)
	} else {
		online, _ = SessionsCtl.CheckRemoteStatus("tcp", hp.RunId, hp.RemoteIP, hp.RemotePort)
	}
	hp.RemotePortStatus = online
}

//grpc
func (sm *SessionsManager) CreateOneHTTP(ctx context.Context, in *pb.HTTPConfig) (*pb.HTTPConfig, error) {
	err := authOpenIoTHubGrpc(ctx, in.RunId)
	if err != nil {
		return in, status.Errorf(codes.Unauthenticated, err.Error())
	}

	return in, sm.AddHttpProxy(&HttpProxy{
		Domain:      in.Domain,
		RunId:       in.RunId,
		RemoteIP:    in.RemoteIP,
		RemotePort:  int(in.RemotePort),
		UserName:    in.UserName,
		Password:    in.Password,
		IfHttps:     in.IfHttps,
		Description: in.Description,
	})
}

func (sm *SessionsManager) DeleteOneHTTP(ctx context.Context, in *pb.HTTPConfig) (*pb.Empty, error) {
	err := authOpenIoTHubGrpc(ctx, in.RunId)
	if err != nil {
		return &pb.Empty{}, status.Errorf(codes.Unauthenticated, err.Error())
	}
	sm.DelHttpProxy(in.Domain)
	return &pb.Empty{}, nil

}

func (sm *SessionsManager) GetOneHTTP(ctx context.Context, in *pb.HTTPConfig) (*pb.HTTPConfig, error) {
	err := authOpenIoTHubGrpc(ctx, in.RunId)
	if err != nil {
		return in, status.Errorf(codes.Unauthenticated, err.Error())
	}
	config, err := sm.GetOneHttpProxy(in.Domain)
	if err != nil {
		return &pb.HTTPConfig{}, err
	}
	return &pb.HTTPConfig{
		Domain:           config.Domain,
		RunId:            config.RunId,
		RemoteIP:         config.RemoteIP,
		RemotePort:       int32(config.RemotePort),
		UserName:         config.UserName,
		Password:         config.Password,
		IfHttps:          config.IfHttps,
		Description:      config.Description,
		RemotePortStatus: config.RemotePortStatus,
	}, err
}

func (sm *SessionsManager) GetAllHTTP(ctx context.Context, in *pb.Device) (*pb.HTTPList, error) {
	var cfgs []*pb.HTTPConfig
	err := authOpenIoTHubGrpc(ctx, in.RunId)
	if err != nil {
		return &pb.HTTPList{HTTPConfigs: cfgs}, status.Errorf(codes.Unauthenticated, err.Error())
	}
	for _, config := range sm.GetAllHttpProxy() {
		if config.RunId == in.RunId && config.RemoteIP == in.Addr {
			cfgs = append(cfgs, &pb.HTTPConfig{
				Domain:           config.Domain,
				RunId:            config.RunId,
				RemoteIP:         config.RemoteIP,
				RemotePort:       int32(config.RemotePort),
				UserName:         config.UserName,
				Password:         config.Password,
				IfHttps:          config.IfHttps,
				Description:      config.Description,
				RemotePortStatus: config.RemotePortStatus,
			})
		}
	}
	return &pb.HTTPList{HTTPConfigs: cfgs}, nil
}

//grpc end

//监听服务
//type Handle struct{}

func (sm *SessionsManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//:TODO 当前不支持websocket的代理（websocket支持basic Auth），或者从beego分发？
	//：TODO 非80端口r.Host的支持情况
	log.Println("host:", r.Host)
	log.Println("hostRequestURI:", r.RequestURI)
	log.Println("hostRequestHEADER:", r.Header)
	log.Println("r.URL.Scheme:", r.URL.Scheme)
	if r.URL.Scheme == "http" {
		log.Println("是http请求")
	} else if r.URL.Scheme == "https" {
		log.Println("是https请求")
	}
	hostInfo, err := sm.GetOneHttpProxy(strings.Split(r.Host, ":")[0])
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	if hostInfo.UserName != "" || hostInfo.Password != "" {
		if u, p, ok := r.BasicAuth(); ok {
			if u != hostInfo.UserName || p != hostInfo.Password {
				w.Header().Set("WWW-Authenticate", `Basic realm="Dotcoo User Login"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		} else {
			w.Header().Set("WWW-Authenticate", `Basic realm="Dotcoo User Login"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	var remote *url.URL
	if hostInfo.IfHttps {
		remote, err = url.Parse(fmt.Sprintf("%s://%s/", "https", r.Host))
	} else {
		remote, err = url.Parse(fmt.Sprintf("%s://%s/", "http", r.Host))
	}

	if err != nil {
		log.Printf(err.Error())
		w.Write([]byte(err.Error()))
		return
	}
	//是websocket
	if v, ok := r.Header["Upgrade"]; ok {
		if v[0] == "websocket" {
			fn := func(ws *websocket.Conn) {
				var pro = "ws"
				var orgpro = "http"
				if hostInfo.IfHttps {
					pro = "wss"
					orgpro = "https"
				}
				conn, err := sm.ConnectToWs(hostInfo.RunId, fmt.Sprintf("%s://%s:%d%s", pro, hostInfo.RemoteIP, hostInfo.RemotePort, r.URL.String()),
					"", fmt.Sprintf("%s://%s:%d%s", orgpro, hostInfo.RemoteIP, hostInfo.RemotePort, r.URL.String()))
				if err != nil {
					log.Printf(err.Error())
					ws.Write([]byte(err.Error()))
					return
				}
				io.Join(ws, conn)
				w.Write([]byte("over"))
				return
			}
			websocket.Handler(fn).ServeHTTP(w, r)
		}
	}
	//是普通http的情况
	proxy := httputil.NewSingleHostReverseProxy(remote)
	var pTransport http.RoundTripper = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		Dial:                  sm.dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	proxy.Transport = pTransport

	proxy.ServeHTTP(w, r)
}

func (sm *SessionsManager) dial(network, address string) (net.Conn, error) {
	//log.Printf("请求的地址addr：%s", address)
	end := strings.Index(address, ":")
	host := address[0:end]
	hostInfo, err := sm.GetOneHttpProxy(host) //id
	if err != nil {
		return nil, err
	}
	var stream *yamux.Stream
	if hostInfo.IfHttps {
		stream, err = sm.ConnectToTls(hostInfo.RunId, hostInfo.RemoteIP, hostInfo.RemotePort)
	} else {
		stream, err = sm.ConnectToTcp(hostInfo.RunId, hostInfo.RemoteIP, hostInfo.RemotePort)
	}
	return stream, err
}
