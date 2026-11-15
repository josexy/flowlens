package settingservice

import (
	"bytes"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// security trust-settings-export uses Apple's XML TrustSettings schema, not
// dump-trust-settings's display text. SHA1 is only the schema's lookup key;
// keychain matching and deletion still use full DER and SHA256 respectively.
// https://github.com/apple-oss-distributions/Security/blob/main/OSX/libsecurity_keychain/lib/TrustSettingsSchema.h
type caTrustPlistValue struct {
	XMLName  xml.Name
	Text     string              `xml:",chardata"`
	Children []caTrustPlistValue `xml:",any"`
}

func (v caTrustPlistValue) dictionary() (map[string]caTrustPlistValue, error) {
	if v.XMLName.Local != "dict" || len(v.Children)%2 != 0 {
		return nil, errors.New("invalid user trust settings dictionary")
	}
	result := make(map[string]caTrustPlistValue, len(v.Children)/2)
	for i := 0; i < len(v.Children); i += 2 {
		key := v.Children[i]
		if key.XMLName.Local != "key" || len(key.Children) != 0 {
			return nil, errors.New("invalid user trust settings key")
		}
		if _, duplicate := result[key.Text]; duplicate {
			return nil, errors.New("duplicate user trust settings key")
		}
		result[key.Text] = v.Children[i+1]
	}
	return result, nil
}

func (v caTrustPlistValue) integer() (int64, error) {
	if v.XMLName.Local != "integer" || len(v.Children) != 0 {
		return 0, errors.New("invalid user trust settings integer")
	}
	return strconv.ParseInt(strings.TrimSpace(v.Text), 10, 64)
}

func (v caTrustPlistValue) data() ([]byte, error) {
	if v.XMLName.Local != "data" || len(v.Children) != 0 {
		return nil, errors.New("invalid user trust settings data")
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(v.Text), ""))
}

func caSecurityTrustSettings(data []byte, cert *x509.Certificate) (present, trusted bool, err error) {
	var plist caTrustPlistValue
	if err := xml.Unmarshal(data, &plist); err != nil {
		return false, false, err
	}
	if plist.XMLName.Local != "plist" || len(plist.Children) != 1 {
		return false, false, errors.New("invalid user trust settings plist")
	}
	root, err := plist.Children[0].dictionary()
	if err != nil {
		return false, false, err
	}
	version, err := root["trustVersion"].integer()
	if err != nil || version != 1 {
		return false, false, errors.New("unsupported user trust settings version")
	}
	list, err := root["trustList"].dictionary()
	if err != nil {
		return false, false, err
	}
	hash := sha1.Sum(cert.Raw)
	var entry caTrustPlistValue
	for key, value := range list {
		if strings.EqualFold(key, hex.EncodeToString(hash[:])) {
			entry, present = value, true
			break
		}
	}
	if !present {
		return false, false, nil
	}
	record, err := entry.dictionary()
	if err != nil {
		return true, false, err
	}
	issuer, err := record["issuerName"].data()
	if err != nil {
		return true, false, err
	}
	serial, err := record["serialNumber"].data()
	if err != nil {
		return true, false, err
	}
	if !bytes.Equal(issuer, cert.RawIssuer) || len(serial) == 0 || new(big.Int).SetBytes(serial).Cmp(cert.SerialNumber) != 0 {
		return true, false, errors.New("user trust settings certificate identity mismatch")
	}
	settings, exists := record["trustSettings"]
	if !exists {
		return true, caSecuritySelfSigned(cert), nil
	}
	if settings.XMLName.Local != "array" {
		return true, false, errors.New("invalid certificate trust settings array")
	}
	if len(settings.Children) == 0 {
		return true, caSecuritySelfSigned(cert), nil
	}
	var allowed uint8
	for _, value := range settings.Children {
		rule, err := value.dictionary()
		if err != nil {
			return true, false, err
		}
		policies, err := caSecurityTrustPolicies(rule)
		if err != nil {
			return true, false, err
		}
		result := int64(1) // Apple's default is trustRoot.
		if v, exists := rule["kSecTrustSettingsResult"]; exists {
			result, err = v.integer()
			if err != nil {
				return true, false, err
			}
		}
		if result == 3 && policies != 0 {
			// A deny entry can affect some clients even if another entry allows
			// TLS. Do not report a universally usable installation in that case.
			return true, false, nil
		}
		if (result == 1 && caSecuritySelfSigned(cert)) || (result == 2 && !caSecuritySelfSigned(cert)) {
			unrestricted, err := caSecurityUnrestrictedRule(rule)
			if err != nil {
				return true, false, err
			}
			if unrestricted {
				allowed |= policies
			}
		}
	}
	// These are the two policies that FlowLens installs, not effective trust
	// inherited from another user's keychain or the administrator domain.
	return true, allowed == 3, nil
}

func caSecurityTrustPolicies(rule map[string]caTrustPlistValue) (uint8, error) {
	policy, exists := rule["kSecTrustSettingsPolicy"]
	if !exists {
		return 3, nil
	}
	var oid []byte
	switch policy.XMLName.Local {
	case "data":
		var err error
		oid, err = policy.data()
		if err != nil {
			return 0, err
		}
	case "string":
		switch policy.Text {
		case "1.2.840.113635.100.1.3":
			return 1, nil
		case "1.2.840.113635.100.1.2":
			return 2, nil
		}
		return 0, nil
	default:
		return 0, errors.New("invalid user trust policy")
	}
	switch {
	case bytes.Equal(oid, []byte{0x2a, 0x86, 0x48, 0x86, 0xf7, 0x63, 0x64, 0x01, 0x03}):
		return 1, nil
	case bytes.Equal(oid, []byte{0x2a, 0x86, 0x48, 0x86, 0xf7, 0x63, 0x64, 0x01, 0x02}):
		return 2, nil
	default:
		return 0, nil
	}
}

func caSecurityUnrestrictedRule(rule map[string]caTrustPlistValue) (bool, error) {
	for key, value := range rule {
		switch key {
		case "kSecTrustSettingsPolicy", "kSecTrustSettingsPolicyName", "kSecTrustSettingsResult", "kSecTrustSettingsModifyDate":
		case "kSecTrustSettingsAllowedError", "kSecTrustSettingsKeyUsage":
			number, err := value.integer()
			if err != nil {
				return false, err
			}
			if (key == "kSecTrustSettingsAllowedError" && number != 0) || (key == "kSecTrustSettingsKeyUsage" && number != -1) {
				return false, nil
			}
		default:
			// App/hostname constraints and unknown future constraints cannot
			// satisfy the general trust installed by this feature.
			return false, nil
		}
	}
	return true, nil
}
