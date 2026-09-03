import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/phone/number_format.dart';

class CallScreen extends StatelessWidget {
  const CallScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final call = appState.activeCall;

    if (call == null) {
      return const SizedBox.shrink();
    }

    // Caller ID: prefer the local contact book, then the name the server sent,
    // then the formatted number.
    final String displayName;
    final String numberLine;
    if (call.isService) {
      displayName = call.serviceName ?? 'Service';
      numberLine = call.serviceCode ?? '';
    } else {
      final formatted = displayNumber(
        canonicalNumber(call.remoteNumber, ownArea: appState.phoneNumber?.areaCode),
      );
      final contact = appState.contactForNumber(call.remoteNumber);
      if (contact != null) {
        displayName = contact.name;
        numberLine = displayNumber(contact.number);
      } else if (call.remoteName.isNotEmpty) {
        displayName = call.remoteName;
        numberLine = formatted;
      } else {
        displayName = formatted.isEmpty ? call.remoteNumber : formatted;
        numberLine = '';
      }
    }

    return Scaffold(
      backgroundColor: Colors.grey[900],
      body: SafeArea(
        child: Column(
          children: [
            const Spacer(flex: 2),

            // Caller info
            Text(
              displayName,
              style: const TextStyle(
                color: Colors.white,
                fontSize: 28,
                fontWeight: FontWeight.w300,
              ),
            ),
            if (numberLine.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  numberLine,
                  style: TextStyle(color: Colors.grey[400], fontSize: 16),
                ),
              ),

            const SizedBox(height: 16),

            // Call status
            Text(
              _statusText(call.state),
              style: TextStyle(color: Colors.grey[400], fontSize: 14),
            ),

            const Spacer(flex: 3),

            // Controls - different for incoming vs outgoing
            if (call.isIncoming && call.state == CallState.ringing)
              _buildIncomingControls(context, appState)
            else
              _buildActiveCallControls(context, appState),
          ],
        ),
      ),
    );
  }

  Widget _buildIncomingControls(BuildContext context, AppState appState) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 48),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceEvenly,
        children: [
          // Decline
          _CallControlButton(
            icon: Icons.call_end,
            label: 'Decline',
            isActive: true,
            color: Colors.red,
            onPressed: () {
              appState.declineCall();
            },
          ),

          // Answer
          _CallControlButton(
            icon: Icons.call,
            label: 'Answer',
            isActive: true,
            color: Colors.green,
            onPressed: () {
              appState.answerCall();
            },
          ),
        ],
      ),
    );
  }

  Widget _buildActiveCallControls(BuildContext context, AppState appState) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 48),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceEvenly,
        children: [
          // Mute
          _CallControlButton(
            icon: appState.isMuted ? Icons.mic_off : Icons.mic,
            label: 'Mute',
            isActive: appState.isMuted,
            onPressed: appState.toggleMute,
          ),

          // Hangup
          _CallControlButton(
            icon: Icons.call_end,
            label: 'End',
            isActive: true,
            color: Colors.red,
            onPressed: () {
              appState.hangup();
            },
          ),

          // Speaker
          _CallControlButton(
            icon: appState.isSpeaker ? Icons.volume_up : Icons.volume_down,
            label: 'Speaker',
            isActive: appState.isSpeaker,
            onPressed: appState.toggleSpeaker,
          ),
        ],
      ),
    );
  }

  String _statusText(CallState state) {
    switch (state) {
      case CallState.calling:
        return 'Calling...';
      case CallState.ringing:
        return 'Ringing...';
      case CallState.connected:
        return 'Connected';
      case CallState.ended:
        return 'Call ended';
      default:
        return '';
    }
  }
}

class _CallControlButton extends StatelessWidget {
  final IconData icon;
  final String label;
  final bool isActive;
  final Color? color;
  final VoidCallback onPressed;

  const _CallControlButton({
    required this.icon,
    required this.label,
    required this.isActive,
    this.color,
    required this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        FloatingActionButton(
          onPressed: onPressed,
          backgroundColor: color ?? (isActive ? Colors.white : Colors.grey[700]),
          child: Icon(
            icon,
            color: color != null ? Colors.white : (isActive ? Colors.black : Colors.white),
          ),
        ),
        const SizedBox(height: 8),
        Text(
          label,
          style: TextStyle(color: Colors.grey[400], fontSize: 12),
        ),
      ],
    );
  }
}
