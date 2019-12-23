##################################
# STEP 1 build executable binary #
##################################
FROM golang:alpine AS builder

# Install git.
# Git is required for fetching the dependencies.
RUN apk update && apk add --no-cache git
COPY . $GOPATH/src/github.com/scotow/musiko

# Move to command directory.
WORKDIR $GOPATH/src/github.com/scotow/musiko/cmd/web

# Fetch dependencies.
# Using go get.
RUN go get -d -v

# Build the binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /go/bin/musiko

##############################
# STEP 2 build a small image #
##############################
FROM alpine

# Add ffmpeg
RUN apk update && apk add --no-cache ffmpeg

# Copy our static executable and static files.
COPY --from=builder /go/bin/musiko /musiko
COPY cmd/web/player /player

# Copy SSL certificates for HTTPS connections.
#COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Run the hello binary.
ENTRYPOINT ["/musiko"]