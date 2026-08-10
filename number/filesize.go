package number

import "math"

// decimalUnits are the SI units, a thousand apart. The list stops at EB because
// an int64 of bytes cannot reach a zettabyte.
var decimalUnits = []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}

// binaryUnits are the IEC units, 1024 apart.
var binaryUnits = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// FileSize renders a byte count in SI units, a thousand bytes to the kilobyte.
//
//	FileSize(1000, 0) // "1 KB"
//	FileSize(1536, 2) // "1.54 KB"
func FileSize(bytes int64, precision int) string {
	return fileSize(bytes, precision, 1000, decimalUnits)
}

// FileSizeBinary renders a byte count in IEC units, 1024 bytes to the kibibyte.
//
//	FileSizeBinary(1024, 0) // "1 KiB"
//	FileSizeBinary(1536, 1) // "1.5 KiB"
func FileSizeBinary(bytes int64, precision int) string {
	return fileSize(bytes, precision, 1024, binaryUnits)
}

// fileSize steps up a unit for every whole multiple of base, so 999 bytes stay
// bytes and 1000 of them become the next unit up.
func fileSize(bytes int64, precision int, base float64, units []string) string {
	v := float64(bytes)
	magnitude := math.Abs(v)
	unit := 0
	for magnitude >= base && unit < len(units)-1 {
		v /= base
		magnitude /= base
		unit++
	}
	return Format(v, precision) + " " + units[unit]
}
