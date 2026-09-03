import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/provision/ocn_ksim_uri.dart';

class RegistrationScreen extends StatefulWidget {
  const RegistrationScreen({super.key});

  @override
  State<RegistrationScreen> createState() => _RegistrationScreenState();
}

class _RegistrationScreenState extends State<RegistrationScreen> {
  final _nameController = TextEditingController();
  final _pasteController = TextEditingController();
  OcnKsimUri? _parsed;
  bool _nameWasPrefilled = false;
  String? _linkError;

  @override
  void dispose() {
    _nameController.dispose();
    _pasteController.dispose();
    super.dispose();
  }

  /// Applies a parsed link. Uses local state so the UI always updates, and
  /// mirrors it into AppState so it survives the connecting/error screen
  /// transitions during activation.
  void _apply(OcnKsimUri link) {
    final appState = context.read<AppState>();
    appState.setProvisionIntent(link);
    setState(() {
      _parsed = link;
      _linkError = null;
      if (link.displayName.isNotEmpty) {
        _nameController.text = link.displayName;
        _nameWasPrefilled = true;
      }
    });
  }

  void _usePastedLink() {
    try {
      debugPrint('Use this link: pasted ${_pasteController.text.length} chars');
      final parsed = OcnKsimUri.parse(_pasteController.text);
      if (parsed == null) {
        setState(() {
          _linkError =
              'That does not look like a valid provisioning link. Copy the full '
              'ocnksim://… URL from the admin panel and paste it here.';
        });
        return;
      }
      debugPrint('Use this link: parsed server=${parsed.serverUrl} tokenLen=${parsed.token.length}');
      _apply(parsed);
    } catch (e, st) {
      debugPrint('Use this link error: $e\n$st');
      setState(() => _linkError = 'Unexpected error: $e');
    }
  }

  Future<void> _scanQr() async {
    final raw = await Navigator.of(context).push<String>(
      MaterialPageRoute(builder: (_) => const _QrScannerPage()),
    );
    if (raw == null || !mounted) return;
    final parsed = OcnKsimUri.parse(raw);
    if (parsed == null) {
      setState(() => _linkError =
          'That QR code is not an ocnksim:// provisioning link.');
      return;
    }
    _apply(parsed);
  }

  void _activate() {
    try {
      final appState = context.read<AppState>();
      final provision = _parsed;
      if (provision == null || _nameController.text.trim().isEmpty) {
        setState(() => _linkError = 'Enter a display name first.');
        return;
      }
      if (appState.status == AppStatus.error) {
        appState.clearError();
      }
      debugPrint('Activating on ${provision.serverUrl} with token len ${provision.token.length}');
      appState.register(
        provision.serverUrl,
        _nameController.text.trim(),
        activationToken: provision.token,
      );
    } catch (e, st) {
      debugPrint('Activate error: $e\n$st');
      setState(() => _linkError = 'Unexpected error: $e');
    }
  }

  void _startOver() {
    context.read<AppState>().clearProvisionIntent();
    _nameController.clear();
    setState(() {
      _parsed = null;
      _nameWasPrefilled = false;
      _linkError = null;
    });
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();

    if (appState.status == AppStatus.connected) {
      return const SizedBox.shrink();
    }

    // Restore from a deep link / pending intent (e.g. after the screen was
    // recreated during the connecting → error transition).
    if (_parsed == null && appState.pendingProvision != null) {
      _parsed = appState.pendingProvision;
      if (_parsed!.displayName.isNotEmpty && !_nameWasPrefilled) {
        _nameController.text = _parsed!.displayName;
      }
    }

    final provision = _parsed;
    final busy = appState.status == AppStatus.connecting;
    final error = appState.status == AppStatus.error ? appState.errorMessage : null;

    return Scaffold(
      appBar: AppBar(title: const Text('Provision phone')),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Icon(Icons.phone_in_talk, size: 64, color: Colors.blue),
            const SizedBox(height: 16),
            const Text(
              'OpenCarrier.Network',
              textAlign: TextAlign.center,
              style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 8),
            Text(
              provision == null
                  ? 'Scan the provisioning QR code from your server admin panel, or paste an ocnksim:// link.'
                  : 'Activate this phone on ${provision.serverUrl}',
              textAlign: TextAlign.center,
              style: TextStyle(color: Colors.grey[600]),
            ),
            const SizedBox(height: 32),

            if (provision == null) ...[
              SizedBox(
                height: 48,
                child: ElevatedButton.icon(
                  onPressed: busy ? null : _scanQr,
                  icon: const Icon(Icons.qr_code_scanner),
                  label: const Text('Scan provisioning QR code'),
                ),
              ),
              const SizedBox(height: 20),
              const Text('or paste a link', textAlign: TextAlign.center),
              const SizedBox(height: 12),
              TextField(
                controller: _pasteController,
                decoration: const InputDecoration(
                  labelText: 'ocnksim:// link',
                  hintText: 'ocnksim://server/?server=ws://…&token=…',
                  border: OutlineInputBorder(),
                  prefixIcon: Icon(Icons.link),
                ),
              ),
              if (_linkError != null) ...[
                const SizedBox(height: 8),
                Text(
                  _linkError!,
                  style: const TextStyle(color: Colors.red, fontSize: 12),
                ),
              ],
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: busy ? null : _usePastedLink,
                child: const Text('Use this link'),
              ),
            ] else ...[
              TextField(
                controller: _nameController,
                enabled: !busy,
                decoration: const InputDecoration(
                  labelText: 'Display Name',
                  hintText: 'Your name (shows on Caller ID)',
                  border: OutlineInputBorder(),
                  prefixIcon: Icon(Icons.person),
                ),
              ),
              const SizedBox(height: 8),
              Text(
                'The number is assigned by the server when you activate.',
                style: TextStyle(color: Colors.grey[500], fontSize: 12),
              ),
              const SizedBox(height: 24),
              if (error != null)
                Container(
                  margin: const EdgeInsets.only(bottom: 16),
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: const Color(0xFFFFEBEE),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        error,
                        style: const TextStyle(color: Color(0xFFB71C1C)),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        'Tap Activate again, or get a fresh QR code from the admin panel.',
                        style: TextStyle(color: Colors.grey[700], fontSize: 12),
                      ),
                    ],
                  ),
                ),
              SizedBox(
                height: 48,
                child: ElevatedButton(
                  onPressed: busy ? null : _activate,
                  child: busy
                      ? const SizedBox(
                          width: 20,
                          height: 20,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Text('Activate phone'),
                ),
              ),
              const SizedBox(height: 8),
              TextButton(
                onPressed: busy ? null : _startOver,
                child: const Text('Start over'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Full-screen QR scanner that returns the first barcode payload it finds.
class _QrScannerPage extends StatefulWidget {
  const _QrScannerPage();

  @override
  State<_QrScannerPage> createState() => _QrScannerPageState();
}

class _QrScannerPageState extends State<_QrScannerPage> {
  final MobileScannerController _controller = MobileScannerController();
  bool _done = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _onDetect(BarcodeCapture capture) {
    if (_done) return;
    for (final barcode in capture.barcodes) {
      final value = barcode.rawValue;
      if (value != null && value.isNotEmpty) {
        _done = true;
        Navigator.of(context).pop(value);
        return;
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(title: const Text('Scan provisioning QR code')),
      body: Stack(
        children: [
          MobileScanner(
            controller: _controller,
            onDetect: _onDetect,
          ),
          const Center(
            child: Text(
              'Point at the QR code',
              style: TextStyle(color: Colors.white, fontSize: 16),
            ),
          ),
        ],
      ),
    );
  }
}
