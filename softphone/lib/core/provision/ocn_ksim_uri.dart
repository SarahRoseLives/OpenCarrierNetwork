/// Parsed contents of an `ocnksim://` provisioning link (the same payload a
/// QR code carries).
class OcnKsimUri {
  final String serverUrl; // e.g. ws://192.168.1.240:9100/ws
  final String token; // one-time activation token
  final String displayName; // optional suggested display name

  const OcnKsimUri({
    required this.serverUrl,
    required this.token,
    this.displayName = '',
  });

  bool get isValid => serverUrl.isNotEmpty && token.isNotEmpty;

  /// Parse a raw deep link string. Returns null if it isn't a valid
  /// ocnksim:// provisioning link.
  static OcnKsimUri? parse(String raw) {
    final text = raw.trim();
    if (!text.toLowerCase().startsWith('ocnksim://')) {
      return null;
    }

    // Treat the part after ocnksim://[host][:port]/ as the query string.
    final schemeIdx = text.indexOf('://');
    if (schemeIdx < 0) return null;
    var rest = text.substring(schemeIdx + 3);

    // Skip host[:port] up to '?' or '/'
    final qIdx = rest.indexOf('?');
    final queryStart = qIdx >= 0 ? qIdx + 1 : -1;
    if (queryStart < 0) {
      // No query parameters at all — nothing useful.
      return null;
    }
    rest = rest.substring(queryStart);

    Uri uri;
    try {
      // Wrap so Uri.parse treats it as a normal uri with a query.
      uri = Uri.parse('http://x/?$rest');
    } catch (_) {
      return null;
    }

    String? server = uri.queryParameters['server'];
    final token = uri.queryParameters['token'] ?? '';
    final name = uri.queryParameters['name'] ?? '';

    if (server == null || server.isEmpty) return null;
    // Normalize ws:// / wss:// (server may arrive url-encoded already decoded
    // by Uri parsing).
    if (!server.startsWith('ws://') && !server.startsWith('wss://')) {
      return null;
    }

    return OcnKsimUri(
      serverUrl: server,
      token: token,
      displayName: _decodeName(name),
    );
  }

  static String _decodeName(String name) {
    try {
      return Uri.decodeComponent(name);
    } catch (_) {
      return name;
    }
  }

  @override
  String toString() => 'OcnKsimUri(server=$serverUrl, token=${token.length} chars)';

  static String? tryParseDeepLink(String? raw) {
    if (raw == null) return null;
    return parse(raw)?.serverUrl;
  }

  Map<String, String> toJson() => {
    'server': serverUrl,
    'token': token,
    if (displayName.isNotEmpty) 'name': displayName,
  };

  String encode() {
    final q = <String, String>{
      'server': serverUrl,
      'token': token,
      if (displayName.isNotEmpty) 'name': displayName,
    };
    final host = Uri.parse(serverUrl).host;
    return 'ocnksim://$host/?${Uri(queryParameters: q).query}';
  }
}
