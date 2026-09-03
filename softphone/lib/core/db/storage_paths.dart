import 'dart:developer';
import 'dart:io';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

/// Resolves a writable base directory for app data, mirroring the fallback
/// behaviour of [UserDatabase] so the same code runs on Android and desktop.
Future<String> appDataBaseDir() async {
  String dirPath;
  try {
    final dir = await getApplicationSupportDirectory();
    dirPath = dir.path;
  } catch (e) {
    log('getApplicationSupportDirectory failed: $e');
    if (Platform.isAndroid) {
      dirPath = '/data/data/dev.sarahsforge.ocnphone/files';
    } else {
      final home = Platform.environment['HOME'] ?? '/tmp';
      dirPath = p.join(home, '.ocnphone');
    }
  }

  final dir = Directory(dirPath);
  if (!await dir.exists()) {
    await dir.create(recursive: true);
  }
  return dirPath;
}
