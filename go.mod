module github.com/octoberswimmer/flow2apex

go 1.25.5

require github.com/octoberswimmer/aer v1.2.3

require (
	git.sr.ht/~jackmordaunt/go-toast v1.1.2 // indirect
	github.com/ForceCLI/force v1.9.0 // indirect
	github.com/ForceCLI/force-md v0.44.0 // indirect
	github.com/ForceCLI/inflect v0.0.0-20130829110746-cc00b5ad7a6a // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/esiqveland/notify v0.13.3 // indirect
	github.com/gen2brain/beeep v0.11.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.3 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackmordaunt/icns/v3 v3.0.1 // indirect
	github.com/nbio/xml v0.0.0-20260302224236-9f64bb3b5a9e // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/octoberswimmer/apexfmt v0.56.1 // indirect
	github.com/octoberswimmer/sformula v0.12.0 // indirect
	github.com/onsi/ginkgo v1.13.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sergeymakinen/go-bmp v1.0.0 // indirect
	github.com/sergeymakinen/go-ico v1.0.0-beta.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tadvi/systray v0.0.0-20190226123456-11a2b8fa57af // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
)

replace github.com/antlr4-go/antlr/v4 => github.com/octoberswimmer/antlr/v4 v4.13.1-octoberswimmer.2

// Force newer split genproto modules to avoid ambiguous import conflicts
// // Required until github.com/sourcegraph/scip updates its dependencies
replace google.golang.org/genproto => google.golang.org/genproto v0.0.0-20240903143218-8af14fe29dc1
