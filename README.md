# res-sniffer

鍩轰簬鏈湴浠ｇ悊鐨勭綉缁滆祫婧愬梾鎺笅杞藉伐鍏凤紝鏀寔 CLI 鍜?HTTP API 涓ょ浣跨敤鏂瑰紡锛屼笓涓?AI Agent 璋冪敤璁捐銆?
## 鐗规€?
- **浠ｇ悊鍡呮帰**锛氶€氳繃鏈湴 HTTP/HTTPS 浠ｇ悊鑷姩鎷︽埅缃戠粶娴侀噺锛岃瘑鍒棰戙€侀煶棰戙€佸浘鐗囥€乵3u8 绛夎祫婧?- **HTTPS 瑙ｅ瘑**锛氬唴缃?CA 璇佷功绠＄悊锛屾敮鎸?HTTPS 娴侀噺涓棿浜鸿В瀵?- **CLI 妯″紡**锛氬畬鏁寸殑鍛戒护琛屾帴鍙ｏ紝鏀寔鍚姩浠ｇ悊銆佺鐞嗕笅杞姐€佺洿鎺ヤ笅杞界瓑鎿嶄綔
- **HTTP API**锛歊ESTful API + WebSocket 瀹炴椂鎺ㄩ€侊紝鏂逛究绋嬪簭鍖栬皟鐢ㄥ拰 AI Agent 闆嗘垚
- **澶氱嚎绋嬩笅杞?*锛氭敮鎸佹櫘閫氭枃浠跺拰 m3u8 瑙嗛鐨勫绾跨▼涓嬭浇
- **璺ㄥ钩鍙?*锛氬熀浜?Go 寮€鍙戯紝鏀寔 Windows銆乵acOS銆丩inux

## 瀹夎

### 浠庢簮鐮佺紪璇?
```bash
git clone https://github.com/shisheng820/res-sniffer.git
cd res-sniffer
go build -o res-sniffer ./cmd/res-sniffer
```

### 鐩存帴涓嬭浇

浠?[Releases](https://github.com/shisheng820/res-sniffer/releases) 椤甸潰涓嬭浇瀵瑰簲骞冲彴鐨勯缂栬瘧浜岃繘鍒舵枃浠躲€?
## 蹇€熷紑濮?
### 1. 瀹夎 CA 璇佷功锛堥娆′娇鐢級

```bash
res-sniffer proxy cert install
```

### 2. 鍚姩浠ｇ悊鍜?API

```bash
res-sniffer start
```

浠ｇ悊榛樿杩愯鍦?`127.0.0.1:8899`锛孉PI 榛樿杩愯鍦?`127.0.0.1:9999`銆?
### 3. 閰嶇疆娴忚鍣?绯荤粺浠ｇ悊

灏嗙郴缁熸垨娴忚鍣ㄤ唬鐞嗚缃负 `127.0.0.1:8899`锛岀劧鍚庢祻瑙堢綉椤碉紝璧勬簮浼氳嚜鍔ㄨ鍡呮帰銆?
### 4. 鏌ョ湅鍜屼笅杞借祫婧?
閫氳繃 API 鏌ョ湅璧勬簮鍒楄〃锛?
```bash
curl http://127.0.0.1:9999/api/resources
```

涓嬭浇璧勬簮锛?
```bash
curl -X POST http://127.0.0.1:9999/api/resources/{id}
```

## CLI 鍛戒护

```bash
# 鍚姩浠ｇ悊鍜?API 鏈嶅姟鍣?res-sniffer start [--proxy-port 8899] [--api-port 9999]

# 浠呭惎鍔?API 鏈嶅姟鍣?res-sniffer serve

# 浠ｇ悊鐩稿叧鎿嶄綔
res-sniffer proxy start      # 鍚姩浠ｇ悊
res-sniffer proxy stop       # 鍋滄浠ｇ悊
res-sniffer proxy cert install  # 瀹夎 CA 璇佷功
res-sniffer proxy cert path     # 鏄剧ず CA 璇佷功璺緞

# 鐩存帴涓嬭浇鎸囧畾 URL
res-sniffer download <url> [-o output.mp4]

# 鏄剧ず鐗堟湰
res-sniffer version
```

## HTTP API

### 鐘舵€?
- `GET /api/status` - 鑾峰彇绯荤粺鐘舵€?
### 浠ｇ悊鎺у埗

- `POST /api/proxy/start` - 鍚姩浠ｇ悊
- `POST /api/proxy/stop` - 鍋滄浠ｇ悊
- `GET /api/proxy/cert` - 涓嬭浇 CA 璇佷功

### 璧勬簮绠＄悊

- `GET /api/resources?type=video&page=1&page_size=20` - 鑾峰彇璧勬簮鍒楄〃
- `GET /api/resources/{id}` - 鑾峰彇璧勬簮璇︽儏
- `DELETE /api/resources/{id}` - 鍒犻櫎璧勬簮
- `DELETE /api/resources` - 娓呯┖鎵€鏈夎祫婧?- `POST /api/resources/{id}` - 浠庤祫婧愬垱寤轰笅杞戒换鍔?
### 涓嬭浇绠＄悊

- `GET /api/downloads?status=running` - 鑾峰彇涓嬭浇鍒楄〃
- `POST /api/downloads` - 浠?URL 鍒涘缓涓嬭浇浠诲姟
- `GET /api/downloads/{id}` - 鑾峰彇涓嬭浇璇︽儏
- `DELETE /api/downloads/{id}` - 鍒犻櫎涓嬭浇
- `POST /api/downloads/{id}/start` - 寮€濮嬩笅杞?- `POST /api/downloads/{id}/pause` - 鏆傚仠涓嬭浇
- `POST /api/downloads/{id}/resume` - 鎭㈠涓嬭浇
- `POST /api/downloads/{id}/cancel` - 鍙栨秷涓嬭浇

### WebSocket

- `ws://127.0.0.1:9999/ws` - 瀹炴椂鎺ㄩ€佹柊璧勬簮鍜屼笅杞借繘搴?
娑堟伅鏍煎紡锛?```json
{
  "type": "resource_new | download_update",
  "data": { ... }
}
```

## 椤圭洰缁撴瀯

```
res-sniffer/
鈹溾攢鈹€ cmd/res-sniffer/    # CLI 鍏ュ彛
鈹溾攢鈹€ internal/
鈹?  鈹溾攢鈹€ proxy/           # 浠ｇ悊鏈嶅姟鍣紙HTTP/HTTPS 涓棿浜猴級
鈹?  鈹溾攢鈹€ sniffer/         # 璧勬簮鍡呮帰
鈹?  鈹溾攢鈹€ downloader/      # 涓嬭浇绠＄悊鍣紙鏅€氭枃浠?+ m3u8锛?鈹?  鈹溾攢鈹€ store/           # 鍐呭瓨鏁版嵁瀛樺偍
鈹?  鈹斺攢鈹€ api/             # HTTP API 鏈嶅姟鍣?鈹溾攢鈹€ pkg/types/           # 鍏叡绫诲瀷瀹氫箟
鈹溾攢鈹€ skill/               # AI Agent Skill
鈹斺攢鈹€ go.mod
```

## 鎶€鏈師鐞?
1. **浠ｇ悊鎷︽埅**锛氬湪鏈湴鍚姩 HTTP 浠ｇ悊鏈嶅姟鍣紝鎷︽埅鎵€鏈夌粡杩囩殑缃戠粶璇锋眰
2. **HTTPS 瑙ｅ瘑**锛氶€氳繃鑷鍚?CA 璇佷功杩涜 TLS 涓棿浜烘敾鍑伙紝瑙ｅ瘑 HTTPS 娴侀噺
3. **璧勬簮璇嗗埆**锛氬垎鏋愬搷搴斿ご鐨?Content-Type 鍜?URL 鍚庣紑锛岃瘑鍒棰戙€侀煶棰戙€佸浘鐗囥€乵3u8 绛夎祫婧?4. **涓嬭浇绠＄悊**锛氭敮鎸佹櫘閫氭枃浠跺绾跨▼涓嬭浇鍜?m3u8 鍒嗙墖涓嬭浇鍚堝苟
5. **API 鎺ュ彛**锛氭彁渚?REST API 鍜?WebSocket锛屾柟渚跨▼搴忓寲璋冪敤

## 涓?res-downloader 鐨勫叧绯?
鏈」鐩熀浜?[res-downloader](https://github.com/putyy/res-downloader) 鐨勬牳蹇冨師鐞嗗紑鍙戯紝浣嗗仛浜嗕互涓嬫敼杩涳細

- 鍘婚櫎 GUI锛屼笓娉?CLI 鍜?API 妯″紡
- 涓撲负 AI Agent 璋冪敤璁捐
- 鏇寸畝娲佺殑浠ｇ爜缁撴瀯
- 鏀寔 WebSocket 瀹炴椂鎺ㄩ€?
## 璁稿彲璇?
MIT License
