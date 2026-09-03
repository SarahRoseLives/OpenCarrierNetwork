import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';

class RegistrationScreen extends StatefulWidget {
  const RegistrationScreen({super.key});

  @override
  State<RegistrationScreen> createState() => _RegistrationScreenState();
}

class _RegistrationScreenState extends State<RegistrationScreen> {
  final _serverController = TextEditingController(text: 'ws://192.168.1.240:9100/ws');
  final _nameController = TextEditingController();
  bool _isLoading = false;

  @override
  void dispose() {
    _serverController.dispose();
    _nameController.dispose();
    super.dispose();
  }

  void _onRegister() {
    if (_nameController.text.isEmpty || _serverController.text.isEmpty) {
      return;
    }

    setState(() => _isLoading = true);
    context.read<AppState>().register(
      _serverController.text,
      _nameController.text,
    );
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();

    if (appState.status == AppStatus.connected) {
      return const SizedBox.shrink();
    }

    return Scaffold(
      appBar: AppBar(title: const Text('OCN Registration')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const Icon(Icons.phone_in_talk, size: 64, color: Colors.blue),
            const SizedBox(height: 24),
            const Text(
              'OpenCarrier.Network',
              style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 8),
            Text(
              'Register with a server to get your phone number',
              style: TextStyle(color: Colors.grey[600]),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 32),

            TextField(
              controller: _serverController,
              decoration: const InputDecoration(
                labelText: 'Server URL',
                hintText: 'ws://server-ip:9100/ws',
                border: OutlineInputBorder(),
                prefixIcon: Icon(Icons.dns),
              ),
            ),
            const SizedBox(height: 16),

            TextField(
              controller: _nameController,
              decoration: const InputDecoration(
                labelText: 'Display Name',
                hintText: 'Your name (shows on Caller ID)',
                border: OutlineInputBorder(),
                prefixIcon: Icon(Icons.person),
              ),
            ),
            const SizedBox(height: 24),

            if (appState.status == AppStatus.error)
              Padding(
                padding: const EdgeInsets.only(bottom: 16),
                child: Text(
                  appState.errorMessage ?? 'Connection failed',
                  style: const TextStyle(color: Colors.red),
                ),
              ),

            SizedBox(
              width: double.infinity,
              height: 48,
              child: ElevatedButton(
                onPressed: _isLoading ? null : _onRegister,
                child: _isLoading
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Register'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
