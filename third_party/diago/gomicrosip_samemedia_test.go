package diago

import "testing"

// sameMedia, the go-microsip rule of checkEarlyMedia: an answer that describes the audio the 183
// described is the same whatever its origin, its session's name, the order of its independent
// attributes or a repeated line; one that changes an address, a port, a codec, the direction, a
// key or the order of two keys is not.
func TestSameMedia(t *testing.T) {
	early := "v=0\r\no=- 1 1 IN IP4 192.0.2.1\r\ns=-\r\nc=IN IP4 192.0.2.1\r\nt=0 0\r\n" +
		"m=audio 40000 RTP/SAVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\n" +
		"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:AAAA\r\na=crypto:2 AES_CM_128_HMAC_SHA1_80 inline:BBBB\r\na=sendrecv\r\n"
	same := map[string]string{
		"another origin and name": "v=0\r\no=- 1 2 IN IP4 192.0.2.1\r\ns=answer\r\nc=IN IP4 192.0.2.1\r\nt=0 0\r\n" +
			"m=audio 40000 RTP/SAVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\n" +
			"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:AAAA\r\na=crypto:2 AES_CM_128_HMAC_SHA1_80 inline:BBBB\r\na=sendrecv\r\n",
		"attributes in another order, one twice": "v=0\r\no=- 1 1 IN IP4 192.0.2.1\r\ns=-\r\nc=IN IP4 192.0.2.1\r\nt=0 0\r\n" +
			"m=audio 40000 RTP/SAVP 0 101\r\na=sendrecv\r\na=rtpmap:101 telephone-event/8000\r\na=rtpmap:0 PCMU/8000\r\n" +
			"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:AAAA\r\na=crypto:2 AES_CM_128_HMAC_SHA1_80 inline:BBBB\r\na=sendrecv\r\n",
	}
	for name, answer := range same {
		if !sameMedia([]byte(early), []byte(answer)) {
			t.Errorf("%s: not the same media", name)
		}
	}
	change := func(old, new string) string {
		out := []byte(early)
		i := indexOf(early, old)
		if i < 0 {
			t.Fatalf("%q is not in the fixture", old)
		}
		return string(out[:i]) + new + early[i+len(old):]
	}
	differ := map[string]string{
		"another address":      change("c=IN IP4 192.0.2.1", "c=IN IP4 192.0.2.9"),
		"another port":         change("m=audio 40000", "m=audio 40002"),
		"another codec":        change("a=rtpmap:0 PCMU/8000", "a=rtpmap:0 PCMA/8000"),
		"other payload types":  change("RTP/SAVP 0 101", "RTP/SAVP 101 0"),
		"another direction":    change("a=sendrecv", "a=sendonly"),
		"a conflicting second": change("a=sendrecv\r\n", "a=sendrecv\r\na=inactive\r\n"),
		"another key":          change("inline:AAAA", "inline:CCCC"),
		"the keys in another order": change(
			"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:AAAA\r\na=crypto:2 AES_CM_128_HMAC_SHA1_80 inline:BBBB",
			"a=crypto:2 AES_CM_128_HMAC_SHA1_80 inline:BBBB\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:AAAA"),
		"another profile": change("RTP/SAVP", "RTP/AVP"),
		"no keys":         change("a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:AAAA\r\na=crypto:2 AES_CM_128_HMAC_SHA1_80 inline:BBBB\r\n", ""),
		"two connections in another order": "v=0\r\no=- 1 1 IN IP4 192.0.2.1\r\ns=-\r\nc=IN IP4 192.0.2.9\r\nc=IN IP4 192.0.2.1\r\nt=0 0\r\n" +
			"m=audio 40000 RTP/SAVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\n" +
			"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:AAAA\r\na=crypto:2 AES_CM_128_HMAC_SHA1_80 inline:BBBB\r\na=sendrecv\r\n",
		"an address in the media section": change("m=audio 40000 RTP/SAVP 0 101\r\n", "m=audio 40000 RTP/SAVP 0 101\r\nc=IN IP4 192.0.2.9\r\n"),
		"nothing":                         "",
	}
	for name, answer := range differ {
		if sameMedia([]byte(early), []byte(answer)) {
			t.Errorf("%s: taken for the same media", name)
		}
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
