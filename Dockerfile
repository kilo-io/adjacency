FROM golang:alpine as build
COPY . /adjacency
WORKDIR /adjacency
RUN CGO_ENABLED=0 go build --mod=vendor -o ./adjacency

FROM scratch
WORKDIR /
COPY --from=build /adjacency/adjacency .
EXPOSE 3000
ENTRYPOINT ["/adjacency"]
