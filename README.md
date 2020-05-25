# adjacency
Check latencies between nodes within a network.

Either get a quadratic (n x n) matrix from all (n) nodes where this service is runnning, or specify a srv name from a diffrent service that runs on m nodes and get a n x m matrix of latencies. 

[![Build Status](https://travis-ci.org/heptoprint/adjacency.svg?branch=master)](https://travis-ci.org/heptoprint/adjacency)

## Usage
Start this service on __one__  node listening on all ips on port 3000 with
```bash
docker run --rm -p 3000:3000 heptoprint/adjacency --listen-address ":3000" --srv "_service._tcp.exmaple.com
```
Do this for all nodes. Keep in mind, that _srv_ needs to resolve to all the urls of the nodes.

To get the quadratic adjancency matrix use curl e.g.
```bash
curl example.com:3000 
```

To get the n x m matrix for the latencies between all nodes of this service and another service's nodes
```bash
curl example.com:3000?srv=_anotherservice._tcp.example.com
```
If you prefer the output as a json string add _&json=true_ to your query. E.g.:
```bash
curl example.com:3000?json=true
```
### More Usage
To only get an adjacency vector from the point of view of a specific node, use
```bash
curl example.com:3000/vector?srv=_service._tcp.example.com
```
This will return a json with urls and latencies.

To check if service is running at all, use
```bash
curl exmaple.com:3000/ping
```
The response sould be _pong_. 
