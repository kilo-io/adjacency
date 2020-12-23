FROM golang:alpine as build
COPY . /adjacency
WORKDIR /adjacency
RUN CGO_ENABLED=0 GOOS=linux go build --mod=vendor -o ./adjacency

FROM scratch
WORKDIR /
COPY --from=build /adjacency/adjacency .
ENTRYPOINT ["/adjacency"]
