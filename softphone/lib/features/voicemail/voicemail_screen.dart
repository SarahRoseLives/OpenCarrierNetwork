import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/db/voicemail_message.dart';
import '../../core/phone/number_format.dart';

class VoicemailScreen extends StatefulWidget {
  const VoicemailScreen({super.key});

  @override
  State<VoicemailScreen> createState() => _VoicemailScreenState();
}

class _VoicemailScreenState extends State<VoicemailScreen> {
  final AudioPlayer _player = AudioPlayer();
  String? _playingId;
  String _busyId = '';
  bool _busy = false;
  bool _started = false;

  @override
  void initState() {
    super.initState();
    _player.onPlayerComplete.listen((_) {
      if (mounted) {
        setState(() => _playingId = null);
      }
    });
  }

  @override
  void dispose() {
    _player.dispose();
    super.dispose();
  }

  Future<void> _toggle(VoicemailMessage message) async {
    final appState = context.read<AppState>();
    if (_playingId == message.id) {
      await _player.stop();
      setState(() => _playingId = null);
      return;
    }
    if (_busy) return;
    setState(() {
      _busy = true;
      _busyId = message.id;
    });
    try {
      final bytes = await appState.fetchVoicemailAudio(message.id);
      await _player.stop();
      await _player.play(BytesSource(bytes));
      if (mounted) {
        setState(() => _playingId = message.id);
      }
      // Playing a message counts as listening to it.
      if (!message.listened) {
        await appState.markVoicemailRead(message.id);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not load voicemail: $e')),
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _delete(VoicemailMessage message) async {
    final appState = context.read<AppState>();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete voicemail'),
        content: const Text('Delete this voicemail?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete', style: TextStyle(color: Colors.red)),
          ),
        ],
      ),
    );
    if (ok == true) {
      await appState.deleteVoicemail(message.id);
    }
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final messages = appState.voicemail;

    if (!_started) {
      _started = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        appState.refreshVoicemail();
      });
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Voicemail')),
      body: messages.isEmpty
          ? RefreshIndicator(
              onRefresh: () async => appState.refreshVoicemail(),
              child: ListView(
                physics: const AlwaysScrollableScrollPhysics(),
                children: const [
                  SizedBox(height: 160),
                  Icon(Icons.voicemail, size: 64, color: Colors.grey),
                  Center(
                    child: Padding(
                      padding: EdgeInsets.all(12),
                      child: Text(
                        'No voicemail',
                        style: TextStyle(color: Colors.grey),
                      ),
                    ),
                  ),
                ],
              ),
            )
          : RefreshIndicator(
              onRefresh: () async => appState.refreshVoicemail(),
              child: ListView.separated(
                physics: const AlwaysScrollableScrollPhysics(),
                itemCount: messages.length,
                separatorBuilder: (_, _) => const Divider(height: 1),
                itemBuilder: (context, i) => _row(appState, messages[i]),
              ),
            ),
    );
  }

  Widget _row(AppState appState, VoicemailMessage message) {
    final isService = isServiceCode(message.callerNumber);
    String title;
    String subtitle;
    if (isService) {
      title = message.callerName.isNotEmpty
          ? message.callerName
          : 'Unknown caller';
      subtitle = message.callerNumber;
    } else {
      final contact = appState.contactForNumber(message.callerNumber);
      if (contact != null) {
        title = contact.name;
      } else if (message.callerName.isNotEmpty) {
        title = message.callerName;
      } else {
        title = displayNumber(message.callerNumber);
      }
      final parts = <String>[];
      if (!isService && message.callerNumber.isNotEmpty) {
        parts.add(displayNumber(message.callerNumber));
      }
      parts.add(_formatDuration(message.durationSeconds));
      subtitle = parts.join(' · ');
    }

    final playing = _playingId == message.id;
    final busy = _busy && _busyId == message.id;

    return ListTile(
      leading: Stack(
        children: [
          CircleAvatar(
            child: Icon(
              Icons.voicemail,
              color: message.listened ? Colors.grey : Colors.blue,
            ),
          ),
          if (!message.listened && !playing)
            const Positioned(
              right: 0,
              child: CircleAvatar(radius: 5, backgroundColor: Colors.red),
            ),
        ],
      ),
      title: Text(
        title,
        style: TextStyle(
          fontWeight: message.listened ? FontWeight.normal : FontWeight.w600,
        ),
      ),
      subtitle: Text(
        subtitle.isEmpty ? _formatClock(message.createdAt) : '$subtitle · ${_formatClock(message.createdAt)}',
        style: const TextStyle(fontSize: 12),
      ),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _PlayButton(
            playing: playing,
            busy: busy,
            onTap: () => _toggle(message),
          ),
          PopupMenuButton<String>(
            onSelected: (v) {
              if (v == 'delete') _delete(message);
            },
            itemBuilder: (_) => const [
              PopupMenuItem(value: 'delete', child: Text('Delete')),
            ],
          ),
        ],
      ),
      onTap: () => _toggle(message),
    );
  }
}

class _PlayButton extends StatelessWidget {
  final bool playing;
  final bool busy;
  final VoidCallback onTap;

  const _PlayButton({
    required this.playing,
    required this.busy,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    if (busy) {
      return const Padding(
        padding: EdgeInsets.all(12),
        child: SizedBox(
          width: 20,
          height: 20,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }
    return IconButton(
      icon: Icon(playing ? Icons.stop : Icons.play_arrow),
      onPressed: onTap,
      tooltip: playing ? 'Stop' : 'Play',
    );
  }
}

String _formatDuration(int sec) {
  final m = (sec ~/ 60).toString().padLeft(2, '0');
  final s = (sec % 60).toString().padLeft(2, '0');
  return '$m:$s';
}

String _formatClock(DateTime t) {
  final h = t.hour.toString().padLeft(2, '0');
  final m = t.minute.toString().padLeft(2, '0');
  return '$h:$m';
}
