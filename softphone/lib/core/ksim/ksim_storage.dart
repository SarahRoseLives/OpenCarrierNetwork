import 'dart:convert';
import 'dart:developer';
import 'dart:math' show Random;
import 'dart:typed_data';
import 'package:cryptography/cryptography.dart';
import '../db/db.dart';
import 'ksim_keypair.dart';

class KSimStorage {
  static final _aesGcm = AesGcm.with256bits();
  static final _pbkdf2 = Pbkdf2(
    macAlgorithm: Hmac.sha256(),
    iterations: 100000,
    bits: 256,
  );

  static Future<void> save({
    required KSimKeypair keypair,
    required String displayName,
    required String serverUrl,
    required String phoneNumber,
    required String passphrase,
    String areaCode = '',
  }) async {
    log('KSimStorage.save: encrypting seed...');
    final encryptedSeed = await _encryptKey(keypair.seed, passphrase);
    log('KSimStorage.save: seed encrypted (${encryptedSeed.length} bytes)');

    await UserDatabase.saveUser(
      publicKey: keypair.publicKey,
      encryptedSeed: encryptedSeed,
      displayName: displayName,
      serverUrl: serverUrl,
      phoneNumber: phoneNumber,
      areaCode: areaCode,
    );
    log('KSimStorage.save: complete');
  }

  static Future<(KSimKeypair, StoredUser)?> load(String passphrase) async {
    log('KSimStorage.load: loading from database...');
    final stored = await UserDatabase.loadUser();
    if (stored == null) {
      log('KSimStorage.load: no stored user');
      return null;
    }

    try {
      log('KSimStorage.load: decrypting seed...');
      final seedBytes = await _decryptKey(stored.encryptedSeed, passphrase);
      log('KSimStorage.load: seed decrypted (${seedBytes.length} bytes)');

      final keypair = KSimKeypair(
        publicKey: stored.publicKey,
        seed: seedBytes,
      );
      log('KSimStorage.load: keypair created');

      return (keypair, stored);
    } catch (e) {
      log('KSimStorage.load: decryption failed: $e');
      return null;
    }
  }

  static Future<bool> exists() async {
    return await UserDatabase.hasUser();
  }

  static Future<void> updatePhoneNumber(String phoneNumber) async {
    await UserDatabase.updateUser(phoneNumber: phoneNumber);
  }

  static Future<void> updateDisplayName(String displayName) async {
    await UserDatabase.updateUser(displayName: displayName);
  }

  static Future<void> deleteAll() async {
    await UserDatabase.deleteUser();
  }

  static Future<Uint8List> _encryptKey(Uint8List data, String passphrase) async {
    final salt = _randomBytes(16);
    final secretKey = await _deriveKey(passphrase, salt);

    final secretBox = await _aesGcm.encrypt(
      data,
      secretKey: secretKey,
    );

    final result = Uint8List(
      salt.length + secretBox.nonce.length + secretBox.cipherText.length + secretBox.mac.bytes.length,
    );
    var offset = 0;
    result.setRange(offset, offset + salt.length, salt);
    offset += salt.length;
    result.setRange(offset, offset + secretBox.nonce.length, secretBox.nonce);
    offset += secretBox.nonce.length;
    result.setRange(offset, offset + secretBox.cipherText.length, secretBox.cipherText);
    offset += secretBox.cipherText.length;
    result.setRange(offset, offset + secretBox.mac.bytes.length, secretBox.mac.bytes);

    return result;
  }

  static Future<Uint8List> _decryptKey(Uint8List encrypted, String passphrase) async {
    const saltLen = 16;
    const nonceLen = 12;
    const macLen = 16;

    if (encrypted.length < saltLen + nonceLen + macLen) {
      throw Exception('Invalid encrypted data');
    }

    final salt = encrypted.sublist(0, saltLen);
    final nonce = encrypted.sublist(saltLen, saltLen + nonceLen);
    final cipherText = encrypted.sublist(saltLen + nonceLen, encrypted.length - macLen);
    final macBytes = encrypted.sublist(encrypted.length - macLen);

    final secretKey = await _deriveKey(passphrase, salt);

    final secretBox = SecretBox(
      cipherText,
      nonce: nonce,
      mac: Mac(macBytes),
    );

    final decrypted = await _aesGcm.decrypt(
      secretBox,
      secretKey: secretKey,
    );

    return Uint8List.fromList(decrypted);
  }

  static Future<SecretKey> _deriveKey(String passphrase, Uint8List salt) async {
    return await _pbkdf2.deriveKey(
      secretKey: SecretKey(utf8.encode(passphrase)),
      nonce: salt,
    );
  }

  static Uint8List _randomBytes(int length) {
    final random = Random.secure();
    return Uint8List.fromList(List.generate(length, (_) => random.nextInt(256)));
  }
}
