import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'dart:typed_data';
import 'package:path_provider/path_provider.dart';
import 'package:path/path.dart' as p;

class UserDatabase {
  static String? _dbPath;
  static Map<String, dynamic>? _cache;

  static Future<String> get _filePath async {
    if (_dbPath != null) return _dbPath!;
    final dir = await getApplicationSupportDirectory();
    _dbPath = p.join(dir.path, 'ocn_user.json');
    log('Database path: $_dbPath');
    return _dbPath!;
  }

  static Future<Map<String, dynamic>> _load() async {
    if (_cache != null) return _cache!;
    final path = await _filePath;
    final file = File(path);
    log('Loading from: $path (exists: ${await file.exists()})');
    if (await file.exists()) {
      try {
        final data = await file.readAsString();
        _cache = jsonDecode(data) as Map<String, dynamic>;
        log('Loaded user data: ${_cache!.keys.join(', ')}');
      } catch (e) {
        log('Error loading user data: $e');
        _cache = {};
      }
    } else {
      _cache = {};
    }
    return _cache!;
  }

  static Future<void> _save() async {
    final path = await _filePath;
    final file = File(path);
    await file.writeAsString(jsonEncode(_cache));
    log('Saved user data to: $path');
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
    log('User saved: $phoneNumber ($displayName)');
  }

  static Future<StoredUser?> loadUser() async {
    final data = await _load();
    if (data.isEmpty || !data.containsKey('public_key')) {
      log('No user data found');
      return null;
    }

    log('Loading user: ${data['phone_number']} (${data['display_name']})');
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
    _dbPath = null;
    final path = await _filePath;
    final file = File(path);
    if (await file.exists()) {
      await file.delete();
      log('Deleted user data');
    }
  }

  static Future<bool> hasUser() async {
    final data = await _load();
    final exists = data.isNotEmpty && data.containsKey('public_key');
    log('hasUser: $exists');
    return exists;
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
