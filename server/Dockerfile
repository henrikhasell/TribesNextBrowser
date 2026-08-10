# TNBrowser backend.
#
# Build context is this directory (server/), not the repository root: the mod
# trees and the .vl2 archives are not part of the server and have no business
# being sent to the daemon.
#
#   docker build -t tnserver server/

FROM golang:1.26-alpine AS build
WORKDIR /src

# Dependencies first, so editing a .go file does not re-download the module
# cache on every build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off makes the binary static, which is what lets the runtime stage below
# be an image with no libc in it. -trimpath keeps build paths out of the
# binary; -s -w drop the symbol table and DWARF, worth a few MB on a program
# that is debugged from its logs.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tnserver ./cmd/tnserver

# static-debian12 carries CA certificates and nothing else -- no shell, no
# package manager, no libc. The certificates are not optional: every session is
# verified by an outbound HTTPS call to TribesNext, so an image without them
# authenticates nobody.
#
# There is no HEALTHCHECK because there is no shell to run one. App Platform
# probes /healthz over HTTP itself; a plain `docker run` should be watched the
# same way.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/tnserver /tnserver

EXPOSE 8080

# CMD rather than ENTRYPOINT: a platform that wants to run a different command
# in this image (a migration job, a one-off) overrides CMD cleanly, whereas an
# ENTRYPOINT would prefix whatever it was given.
CMD ["/tnserver"]
