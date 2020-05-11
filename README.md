# adjacency_service
Create an adjacency matrix for one srv name. So if that name resolves to 5 urls, a 5x5 matrix will be created. This service needs to be running on all nodes.

## Compile
Compile with 
```bash
go build
```

## Usage
Start this service on __one__  nodes with
```bash
adjacency_service --listen-address "example.com:3000" --srv "_service._tcp.exmaple.com
```
Do this for all nodes. Keep in mind, that the _listen-address_ should be different for all nodes and the _srv_ needs to resolve to all the urls of the nodes.

To get the adjancency matrix use curl e.g.
```bash
curl example.com:3000 
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
