package main

import (
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"
)

func TestPad(t *testing.T) {
	l1 := Latency{
		Duration:    1 * time.Second,
		Ok:          true,
		Destination: "10-0-0-1.example.com",
		IP:          "10.0.0.1",
	}
	l2 := Latency{
		Duration:    5 * time.Second,
		Ok:          true,
		Destination: "10-0-0-2.example.com",
		IP:          "10.0.0.2",
	}
	l3 := Latency{
		Duration:    3 * time.Second,
		Ok:          true,
		Destination: "10-0-0-3.example.com",
		IP:          "10.0.0.3",
	}
	l4 := Latency{
		Duration:    3 * time.Second,
		Ok:          true,
		Destination: "10-0-0-4.example.com",
		IP:          "10.0.0.4",
	}
	ld := Latency{
		Destination: "dummy",
	}
	v1 := Vector{
		Source:    "10-0-0-1.example.com",
		IP:        "10.0.0.1",
		Latencies: []Latency{l1, l2, l3},
	}
	v2 := Vector{
		Source:    "10-0-0-2.example.com",
		IP:        "10.0.0.2",
		Latencies: []Latency{l2, l3, l1},
	}
	v3 := Vector{
		Source:    "10-0-0-3.example.com",
		IP:        "10.0.0.3",
		Latencies: []Latency{l3, l2, l1},
	}
	v4 := v2
	v5 := v3
	v4.Latencies = v1.Latencies
	v5.Latencies = v1.Latencies
	v6 := v1
	v6.Latencies = []Latency{l2, l1, l3, l4}
	v7 := v1
	v7.Latencies = []Latency{l1, l2, l3, l4}
	v8 := v2
	v8.Latencies = []Latency{l1, l2, l3, ld}
	for i, m := range []struct {
		m  matrix
		em matrix
	}{
		{
			m:  matrix{v1, v1, v1},
			em: matrix{v1, v1, v1},
		},
		{
			m:  matrix{v2, v1, v3},
			em: matrix{v1, v4, v5},
		},
		{
			m:  matrix{v6},
			em: matrix{v7},
		},
		{
			m:  matrix{v2, v6},
			em: matrix{v7, v8},
		},
		{
			m: matrix{
				Vector{
					Source: "2",
					Latencies: []Latency{
						{
							Destination: "3",
							Ok:          true,
							Duration:    1 * time.Second,
						},
						{
							Destination: "1",
							Ok:          true,
							Duration:    1 * time.Second,
						},
					},
				},
				Vector{
					Source: "1",
					Latencies: []Latency{
						{
							Destination: "3",
							Ok:          true,
							Duration:    1 * time.Second,
						},
						{
							Destination: "2",
							Ok:          true,
							Duration:    1 * time.Second,
						},
						{
							Destination: "4",
							Ok:          true,
							Duration:    1 * time.Second,
						},
					},
				},
			},
			em: matrix{
				Vector{
					Source: "1",
					Latencies: []Latency{
						{
							Destination: "dummy",
						},
						{
							Destination: "2",
							Ok:          true,
							Duration:    1 * time.Second,
						},
						{
							Destination: "3",
							Ok:          true,
							Duration:    1 * time.Second,
						},
						{
							Destination: "4",
							Ok:          true,
							Duration:    1 * time.Second,
						},
					},
				},
				Vector{
					Source: "2",
					Latencies: []Latency{
						{
							Destination: "1",
							Ok:          true,
							Duration:    1 * time.Second,
						},
						{
							Destination: "dummy",
						},
						{
							Destination: "3",
							Ok:          true,
							Duration:    1 * time.Second,
						},
						{
							Destination: "dummy",
						},
					},
				},
			},
		},
	} {

		if diff := pretty.Compare(m.m.Pad(), m.em); diff != "" {
			t.Errorf("test cast %d failed:\n%v", i, diff)
		}
	}
}
