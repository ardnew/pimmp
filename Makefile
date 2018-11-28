# ==============================================================================
#
#  ENVIRONMENT & PROJECT CONFIGURATION
#
# ==============================================================================
#

# -- static project definitions ------------------------------------------------

project    = pimmp
configpath = $(HOME)/.$(project)
importpath = ardnew.com/$(project)
gopathsrc  = $(GOPATH)/src
gopathbin  = $(GOPATH)/bin

runverbose = -verbose

# -- define version info with version control ----------------------------------

version   = 0.1
revision  = r$(shell svn info| \grep -oP '^Revision:\s*\K\d+')
buildtime = $(shell date -u '+%FT%TZ')

# -- go flags (see: go help build) ---------------------------------------------

goflags-release =
#goflags         = -race
goflags         =

# -- compiler flags (see: go tool compile -help) -------------------------------

gcflags-release =
gcflags         = all='-N -l'

# -- linker flags (see: go tool link -help) ------------------------------------

ldflags-version = -X "main.identity=$(project)" -X "main.version=$(version)" -X "main.revision=$(revision)" -X "main.buildtime=$(buildtime)"
ldflags-release = '-w -s $(ldflags-version)'
ldflags         = '$(ldflags-version)'



# ==============================================================================
#
#  TARGET DEFINITION
#
# ==============================================================================
#

# -- janitorial / cleanup targets ----------------------------------------------

.PHONY: rinse clean scrub sync-ripper-push sync-ripper-pull

rinse:
	rm -rf "$(configpath)"

clean:
	rm -f "$(gopathsrc)/$(importpath)/$(project)"
	rm -f "$(gopathbin)/$(project)"

scrub: rinse clean

sync-ripper-push:
	rsync -rave 'ssh -p 2222 -l andrew' $(gopathsrc)/$(importpath)/ ardnew.com:$(shell ssh ripper 'echo $$GOPATH/src | sed -E "s|^$$HOME|~|"')/$(importpath)

sync-ripper-pull:
	rsync -rave 'ssh -p 2222 -l andrew' ardnew.com:$(shell ssh ripper 'echo $$GOPATH/src | sed -E "s|^$$HOME|~|"')/$(importpath)/ $(gopathsrc)/$(importpath)

# -- compilation targets -------------------------------------------------------

.PHONY: build install

build:
	go build $(goflags) -gcflags=$(gcflags) -ldflags=$(ldflags) "$(importpath)"

install:
	go install $(goflags) -gcflags=$(gcflags) -ldflags=$(ldflags) "$(importpath)"

# -- test / evaluation targets -------------------------------------------------

.PHONY: debug-single-lib debug-dual-lib

debug-single-lib: install
	dlv exec $(project) -- $(runverbose) /mnt/SG4TB-NIX

debug-dual-lib: install
	dlv exec $(project) -- $(runverbose) /mnt/SG4TB-NIX/movies /mnt/SG4TB-NIX/tv
