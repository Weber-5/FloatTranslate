package credential

import "os"

// windowsE2EEnabled gates the real Credential Manager test.
var windowsE2EEnabled = os.Getenv("FT_CRED_E2E") == "1"
