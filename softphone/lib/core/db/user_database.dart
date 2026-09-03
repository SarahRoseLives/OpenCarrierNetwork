import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'dart:typed_data';
import 'package:path_provider/path_provider.dart';
import 'package:path/path.dart' as p;

class UserDatabase {
  static Map<String, dynamic>? _cache;

  static Future<String> get _filePath async {
    String dirPath;

    try {
      final dir = await getApplicationSupportDirectory();
      dirPath = dir.path;
    } catch (e) {
      log('getApplicationSupportDirectory failed: $e');
      // Fallback
      if (Platform.isAndroid) {
        dirPath = '/data/data/dev.sarahsforge.ocnphone/files';
      } else {
        final home = Platform.environment['HOME'] ?? '/tmp';
        dirPath = p.join(home, '.ocnphone');
      }
    }

    // Ensure directory exists
    final dir = Directory(dirPath);
    if (!await dir.exists()) {
      await dir.create(recursive: true);
    }

    return p.join(dirPath, 'ocn_user.json');
  }

  static Future<Map<String, dynamic>> _load() async {
    if (_cache != null) return _cache!;

    try {
      final path = await _filePath;
      final file = File(path);

      if (await file.exists()) {
        final data = await file.readAsString();
        if (data.isNotEmpty) {
          _cache = jsonDecode(data) as Map<String, dynamic>;
          return _cache!;
        }
      }
    } catch (e) {
      log('UserDatabase._load error: $e');
    }

    _cache = {};
    return _cache!;
  }

  static Future<void> _save() async {
    try {
      final path = await _filePath;
      final file = File(path);

      // Ensure parent directory exists
      final dir = file.parent;
      if (!await dir.exists()) {
        await dir.create(recursive: true);
      }

      await file.writeAsString(jsonEncode(_cache));
      log('Saved to: $path');
    } catch (e) {
      log('UserDatabase._save error: $e');
      rethrow;
    }
  }

  static Future<void> saveUser({
    required Uint8List publicKey,
    required Uint8List encryptedSeed,
    required String displayName,
    required String serverUrl,
    required String phoneNumber,
    required String areaCode,
  }) async {
    _cache = {
      'public_key': base64Encode(publicKey),
      'encrypted_seed': base64Encode(encryptedSeed),
      'display_name': displayName,
      'server_url': serverUrl,
      'phone_number': phoneNumber,
      'area_code': areaCode,
      'registered_at': DateTime.now().toIso8601String(),
    };
    await _save();
  }

  static Future<StoredUser?> loadUser() async {
    final data = await _load();
    if (data.isEmpty || !data.containsKey('public_key')) {
      return null;
    }

    return StoredUser(
      publicKey: base64Decode(data['public_key'] as String),
      encryptedSeed: base64Decode(data['encrypted_seed'] as String),
      displayName: data['display_name'] as String? ?? '',
      serverUrl: data['server_url'] as String? ?? '',
      phoneNumber: data['phone_number'] as String? ?? '',
      areaCode: data['area_code'] as String? ?? '',
      registeredAt: data['registered_at'] as String? ?? '',
    );
  }

  static Future<void> updateUser({
    String? displayName,
    String? phoneNumber,
    String? areaCode,
  }) async {
    final data = await _load();
    if (data.isEmpty) return;
    if (displayName != null) data['display_name'] = displayName;
    if (phoneNumber != null) data['phone_number'] = phoneNumber;
    if (areaCode != null) data['area_code'] = areaCode;
    await _save();
  }

  static Future<void> deleteUser() async {
    _cache = {};
    try {
      final path = await _filePath;
      final file = File(path);
      if (await file.exists()) {
        await file.delete();
      }
    } catch (e) {
      log('UserDatabase.deleteUser error: $e');
    }
  }

  static Future<bool> hasUser() async {
    final data = await _load();
    return data.isNotEmpty && data.containsKey('public_key');
  }
}

class StoredUser {
  final Uint8List publicKey;
  final Uint8List encryptedSeed;
  final String displayName;
  final String serverUrl;
  final String phoneNumber;
  final String areaCode;
  final String registeredAt;

  StoredUser({
    required this.publicKey,
    required this.encryptedSeed,
    required this.displayName,
    required this.serverUrl,
    required this.phoneNumber,
    required this.areaCode,
    required this.registeredAt,
  });
}
