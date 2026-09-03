import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/audio/dtmf_tones.dart';
import 'dial_number_format.dart';

class DialerScreen extends StatefulWidget {
  const DialerScreen({super.key});

  @override
  State<DialerScreen> createState() => _DialerScreenState();
}

class _DialerScreenState extends State<DialerScreen> {
  String _dialNumber = '';

  void _onDigitPressed(String digit) {
    DtmfTones.play(digit);
    setState(() {
      _dialNumber += digit;
    });
  }

  void _onBackspace() {
    if (_dialNumber.isNotEmpty) {
      setState(() {
        _dialNumber = _dialNumber.substring(0, _dialNumber.length - 1);
      });
    }
  }

  void _onCall() {
    if (_dialNumber.isNotEmpty) {
      context.read<AppState>().makeCall(_dialNumber);
    }
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();

    // Show error snackbar if there's an error message
    if (appState.errorMessage != null && appState.errorMessage!.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(appState.errorMessage!),
            backgroundColor: Colors.red,
            duration: const Duration(seconds: 3),
          ),
        );
        appState.clearError();
      });
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('OCN Phone'),
        actions: [
          if (appState.phoneNumber != null)
            Padding(
              padding: const EdgeInsets.all(8.0),
              child: Center(
                child: Text(
                  appState.phoneNumber!.formatted,
                  style: const TextStyle(fontSize: 14),
                ),
              ),
            ),
        ],
      ),
      body: Column(
        children: [
          // Reconnecting banner
          if (appState.isReconnecting)
            Container(
              width: double.infinity,
              color: Colors.orange,
              padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 16),
              child: const Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Colors.white,
                    ),
                  ),
                  SizedBox(width: 12),
                  Text(
                    'Reconnecting to server...',
                    style: TextStyle(color: Colors.white, fontSize: 14),
                  ),
                ],
              ),
            ),

          // Number display
          Container(
            padding: const EdgeInsets.all(24),
            width: double.infinity,
            child: Text(
              formatDialNumber(_dialNumber),
              textAlign: TextAlign.center,
              style: const TextStyle(fontSize: 32, fontWeight: FontWeight.w300),
            ),
          ),

          const Spacer(),

          // Dial pad
          _buildDialPad(),

          const Spacer(),

          // Call button
          Padding(
            padding: const EdgeInsets.only(bottom: 32),
            child: Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                FloatingActionButton(
                  onPressed: _dialNumber.isNotEmpty && !appState.isReconnecting ? _onCall : null,
                  backgroundColor: appState.isReconnecting ? Colors.grey : Colors.green,
                  child: const Icon(Icons.call, color: Colors.white),
                ),
                if (_dialNumber.isNotEmpty) ...[
                  const SizedBox(width: 24),
                  FloatingActionButton(
                    onPressed: _onBackspace,
                    backgroundColor: Colors.grey[300],
                    child: const Icon(Icons.backspace, color: Colors.black54),
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildDialPad() {
    final digits = [
      ['1', '2', '3'],
      ['4', '5', '6'],
      ['7', '8', '9'],
      ['*', '0', '#'],
    ];

    return Column(
      children: digits.map((row) {
        return Row(
          mainAxisAlignment: MainAxisAlignment.spaceEvenly,
          children: row.map((digit) {
            return _DialButton(
              digit: digit,
              onPressed: () => _onDigitPressed(digit),
            );
          }).toList(),
        );
      }).toList(),
    );
  }
}

class _DialButton extends StatelessWidget {
  final String digit;
  final VoidCallback onPressed;

  const _DialButton({required this.digit, required this.onPressed});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(8),
      child: SizedBox(
        width: 72,
        height: 72,
        child: Material(
          color: Colors.grey[200],
          shape: const CircleBorder(),
          child: InkWell(
            onTap: onPressed,
            customBorder: const CircleBorder(),
            child: Center(
              child: Text(
                digit,
                style: const TextStyle(fontSize: 28, fontWeight: FontWeight.w400),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
