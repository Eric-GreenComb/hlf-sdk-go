package util

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/pkg/errors"
	"go.opencensus.io/trace"
	"go.uber.org/zap"

	"github.com/s7techlab/hlf-sdk-go/api/config"
	"github.com/s7techlab/hlf-sdk-go/opencensus/hlf"
	"github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

var (
	retryDefaultConfig = config.GRPCRetryConfig{
		Max:     10,
		Timeout: config.Duration{Duration: 10 * time.Second},
	}
)

const (
	maxRecvMsgSize = 100 * 1024 * 1024
	maxSendMsgSize = 100 * 1024 * 1024
)

func NewGRPCOptionsFromConfig(c config.ConnectionConfig, log *zap.Logger) ([]grpc.DialOption, error) {
	l := log.Named(`NewGRPCOptionsFromConfig`)

	// TODO: move to config or variable options
	grpcOptions := []grpc.DialOption{
		grpc.WithStatsHandler(hlf.Wrap(&ocgrpc.ClientHandler{
			StartOptions: trace.StartOptions{
				Sampler:  trace.AlwaysSample(),
				SpanKind: trace.SpanKindClient,
			},
		})),
	}

	if c.Tls.Enabled {
		l.Debug(`Using TLS credentials`)
		var cred credentials.TransportCredentials
		var err error
		if c.Tls.CertPath != `` {
			if cred, err = credentials.NewClientTLSFromFile(c.Tls.CertPath, ``); err != nil {
				l.Error(`Failed to read TLS credentials`, zap.Error(err))
				return nil, errors.Wrap(err, `failed to read tls credentials`)
			} else {

			}
		} else {
			var addr string
			if c.Tls.HostOverride != `` {
				l.Debug(`Overriding TLS host`, zap.String(`host`, c.Tls.HostOverride))
				if addr, _, err = net.SplitHostPort(c.Tls.HostOverride); err != nil {
					return nil, errors.Wrap(err, `failed to parse override tls host`)
				}
			} else {
				l.Debug(`Using TLS host`, zap.String(`host`, c.Host))
				if addr, _, err = net.SplitHostPort(c.Host); err != nil {
					return nil, errors.Wrap(err, `failed to parse tls host`)
				}
			}

			cred = credentials.NewTLS(&tls.Config{ServerName: addr})
		}

		l.Debug(`Read TLS credentials`, zap.Reflect(`cred`, cred.Info()))
		grpcOptions = append(grpcOptions, grpc.WithTransportCredentials(cred))
	} else {
		l.Debug(`TLS is not used`)
		grpcOptions = append(grpcOptions, grpc.WithInsecure())
	}

	// Set KeepAlive parameters if presented
	if c.GRPC.KeepAlive != nil {
		l.Debug(`Using KeepAlive params`,
			zap.Duration(`time`, time.Duration(c.GRPC.KeepAlive.Time)*time.Second),
			zap.Duration(`timeout`, time.Duration(c.GRPC.KeepAlive.Timeout)*time.Second),
		)
		grpcOptions = append(grpcOptions, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                time.Duration(c.GRPC.KeepAlive.Time) * time.Second,
			Timeout:             time.Duration(c.GRPC.KeepAlive.Timeout) * time.Second,
			PermitWithoutStream: true,
		}))
	} else {
		grpcOptions = append(grpcOptions, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}))
	}

	var retryConfig *config.GRPCRetryConfig
	if c.GRPC.Retry != nil {
		l.Debug(`Using presented GRPC retry config`, zap.Reflect(`config`, *c.GRPC.Retry))
		retryConfig = c.GRPC.Retry
	} else {
		l.Debug(`Using default GRPC retry config`, zap.Reflect(`config`, retryDefaultConfig))
		retryConfig = &retryDefaultConfig
	}

	grpcOptions = append(grpcOptions,
		grpc.WithUnaryInterceptor(
			grpc_retry.UnaryClientInterceptor(
				grpc_retry.WithMax(retryConfig.Max),
				grpc_retry.WithBackoff(grpc_retry.BackoffLinear(retryConfig.Timeout.Duration)),
			),
		),
	)

	grpcOptions = append(grpcOptions, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(maxRecvMsgSize),
		grpc.MaxCallSendMsgSize(maxSendMsgSize),
	))

	return grpcOptions, nil
}
