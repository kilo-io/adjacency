# adjacency_service
Check latencies of nodes within a network.

Either get a quadratic (n x n) matrix from all (n) nodes where this service is runnning, or specify a srv name from a diffrent service that runs on m nodes and get a n x m matrix of latencies. 

## Compile
Compile with 
```bash
go build
```

## Usage
Start this service on __one__  node listening on all ips on port 3000 with
```bash
adjacency_service --listen-address ":3000" --srv "_service._tcp.exmaple.com
```
Do this for all nodes. Keep in mind, that _srv_ needs to resolve to all the urls of the nodes.

To get the quadratic adjancency matrix use curl e.g.
```bash
curl example.com:3000 
```

To get the n x m matrix for the latencies between all nodes of this service and another service's nodes
```bash
curl example.com:3000?srv=_service._tcp.anotherservice.com
```
### More Usage
To only get an adjacency vector of a specific node, use
```bash
curl exmaple.com:3000/vector
```
if service is listening to _exmaple.com:3000_. This will return a json with url and latency.

To check if service is running at all, use
```bash
curl exmaple.com:3000/ping
```
The response sould be _pong_. 
