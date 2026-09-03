import 'dart:convert';
import 'dart:typed_data';
import 'package:cryptography/cryptography.dart';
import 'package:crypto/crypto.dart' as crypto;

class KSimKeypair {
  final Uint8List publicKey;
  final Uint8List seed; // 32-byte Ed25519 seed

  KSimKeypair({required this.publicKey, required this.seed});

  static final _ed25519 = Ed25519();

  static Future<KSimKeypair> generate() async {
    final keyPair = await _ed25519.newKeyPair();
    final publicKeyBytes = await keyPair.extractPublicKey();
    final privateKeyBytes = await keyPair.extract();

    // extract() returns the full private key, seed is the first 32 bytes
    final seedBytes = Uint8List.fromList(privateKeyBytes.bytes.sublist(0, 32));

    return KSimKeypair(
      publicKey: Uint8List.fromList(publicKeyBytes.bytes),
      seed: seedBytes,
    );
  }

  Future<Uint8List> sign(Uint8List data) async {
    final keyPair = await _ed25519.newKeyPairFromSeed(seed);
    final signature = await _ed25519.sign(data, keyPair: keyPair);
    return Uint8List.fromList(signature.bytes);
  }

  static Future<bool> verify(Uint8List publicKey, Uint8List data, Uint8List signature) async {
    try {
      final publicKeyObj = SimplePublicKey(publicKey, type: KeyPairType.ed25519);
      return await _ed25519.verify(
        data,
        signature: Signature(signature, publicKey: publicKeyObj),
      );
    } catch (_) {
      return false;
    }
  }

  Future<Uint8List> signChallenge(Uint8List nonce, int timestamp) async {
    final data = buildChallengeData(nonce, timestamp);
    return await sign(data);
  }

  static Uint8List buildChallengeData(Uint8List nonce, int timestamp) {
    // Must match Go: SHA256(nonce || big-endian timestamp)
    final tsBytes = ByteData(8)..setInt64(0, timestamp, Endian.big);
    final combined = Uint8List(nonce.length + 8);
    combined.setRange(0, nonce.length, nonce);
    combined.setRange(nonce.length, nonce.length + 8, tsBytes.buffer.asUint8List());

    // SHA256 hash to match Go implementation
    final hash = crypto.sha256.convert(combined);
    return Uint8List.fromList(hash.bytes);
  }

  String encodePublicKey() => base64Encode(publicKey);
  String encodeSeed() => base64Encode(seed);

  static Uint8List decodePublicKey(String encoded) => base64Decode(encoded);
  static Uint8List decodeSeed(String encoded) => base64Decode(encoded);
}
