FROM golang:alpine as build
ARG ARCH=amd64
COPY . /adjacency
WORKDIR /adjacency
RUN CGO_ENABLED=0 GOARCH=$ARCH GOOS=linux go build --mod=vendor -o ./adjacency

FROM scratch
WORKDIR /
COPY --from=build /adjacency/adjacency .
EXPOSE 3000
ENTRYPOINT ["/adjacency"]
