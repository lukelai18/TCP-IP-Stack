package ip

import (
	"github.com/google/netstack/tcpip/header"
)

// Compute the checksum using the netstack package
func ComputeChecksum(b []byte) uint16 {
	checksum := header.Checksum(b, 0)

	// Invert the checksum value.  Why is this necessary?
	// This function returns the inverse of the checksum
	// on an initial computation.  While this may seem weird,
	// it makes it easier to use this same function
	// to validate the checksum on the receiving side.
	// See ValidateChecksum in the receiver file for details.
	checksumInv := checksum ^ 0xffff

	return checksumInv
}

// Validate the checksum using the netstack package
// Here, we provide both the byte array for the header AND
// the initial checksum value that was stored in the header
//
// "Why don't we need to set the checksum value to 0 first?"
//
// Normally, the checksum is computed with the checksum field
// of the header set to 0.  This library creatively avoids
// this step by instead subtracting the initial value from
// the computed checksum.
// If you use a different language or checksum function, you may
// need to handle this differently.
func ValidateChecksum(b []byte, fromHeader uint16) uint16 {
	checksum := header.Checksum(b, fromHeader)

	return checksum
}
