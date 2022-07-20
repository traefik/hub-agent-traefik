module github.com/traefik/hub-agent-traefik

go 1.18

require (
	github.com/abbot/go-http-auth v0.4.0
	github.com/docker/cli v20.10.17+incompatible
	github.com/docker/docker v20.10.17+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/ettle/strcase v0.1.1
	github.com/evanphx/json-patch v0.5.2
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/gorilla/websocket v1.5.0
	github.com/hamba/avro v1.8.0
	github.com/hashicorp/go-retryablehttp v0.7.1
	github.com/pquerna/cachecontrol v0.1.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.35.0
	github.com/rs/zerolog v1.27.0
	github.com/stretchr/testify v1.7.5
	github.com/traefik/genconf v0.2.0
	github.com/urfave/cli/v2 v2.10.3
	github.com/vulcand/predicate v1.2.0
	golang.org/x/sync v0.0.0-20220601150217-0de741cfad7f
	gopkg.in/square/go-jose.v2 v2.6.0
)

require (
	github.com/Microsoft/go-winio v0.5.2 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/gogo/protobuf v1.1.1 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.6 // indirect
	github.com/gravitational/trace v1.1.16-0.20220114165159-14a9a7dd6aaf // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/yamux v0.0.0-20211028200310-0bc27b27de87
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/moby/term v0.0.0-20210619224110-3f7ff695adc6 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/stretchr/objx v0.4.0 // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	golang.org/x/crypto v0.0.0-20211215165025-cf75a172585e // indirect
	golang.org/x/net v0.0.0-20220225172249-27dd8689420f // indirect
	golang.org/x/sys v0.0.0-20220615213510-4f61da869c0c // indirect
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.2.0 // indirect
)

replace github.com/abbot/go-http-auth => github.com/containous/go-http-auth v0.4.1-0.20210329152427-e70ce7ef1ade
