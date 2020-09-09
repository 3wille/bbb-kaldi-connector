# bbb-kaldi-connector

This project relays audio from freeswitch to a [Kaldi-Model-Server](https://github.com/uhh-lt/kaldi-model-server) over redis. Audio is received over RTP as OPUS, decoded and send on as PCM 16bit.

## Usage 

```
go run bbb_session_token freeswitch_token
```
