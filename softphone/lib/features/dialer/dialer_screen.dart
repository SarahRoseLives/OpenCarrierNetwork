import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';

class DialerScreen extends StatefulWidget {
  const DialerScreen({super.key});

  @override
  State<DialerScreen> createState() => _DialerScreenState();
}

class _DialerScreenState extends State<DialerScreen> {
  String _dialNumber = '';

  void _onDigitPressed(String digit) {
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
          // Number display
          Container(
            padding: const EdgeInsets.all(24),
            width: double.infinity,
            child: Text(
              _formatDialNumber(_dialNumber),
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
                  onPressed: _dialNumber.isNotEmpty ? _onCall : null,
                  backgroundColor: Colors.green,
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

  String _formatDialNumber(String number) {
    if (number.length <= 3) return number;
    if (number.length <= 7) {
      return '${number.substring(0, 3)}-${number.substring(3)}';
    }
    return '${number.substring(0, 3)}-${number.substring(3, 7)}-${number.substring(7)}';
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
