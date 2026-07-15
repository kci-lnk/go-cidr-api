package main

import (
	"flag"
	"fmt"
	"log"

	"go-cidr-api/internal/cidrgen"
)

func main() {
	v4File := flag.String("v4-xdb", "ipdata/base_full_v4.xdb", "path to the IPv4 XDB file")
	v6File := flag.String("v6-xdb", "ipdata/base_full_v6.xdb", "path to the IPv6 XDB file")
	output := flag.String("output", "china_city_cidrs.compact.json", "path to the generated compact JSON")
	flag.Parse()

	_, summary, err := cidrgen.Generate(cidrgen.Options{
		IPv4File: *v4File,
		IPv6File: *v6File,
		Output:   *output,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("generated %s: %d province nodes, %d cities, %d IPv4 CIDRs, %d IPv6 CIDRs\n",
		*output, summary.Provinces, summary.Cities, summary.IPv4CIDRs, summary.IPv6CIDRs)
}
