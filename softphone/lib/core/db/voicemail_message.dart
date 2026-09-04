/// A voicemail message as returned by the server.
class VoicemailMessage {
  final String id;
  String callerNumber; // canonical digits
  String callerName;
  final int durationSeconds;
  bool listened;
  final DateTime createdAt;

  VoicemailMessage({
    required this.id,
    required this.callerNumber,
    required this.callerName,
    required this.durationSeconds,
    required this.listened,
    required this.createdAt,
  });

  VoicemailMessage copyWith({bool? listened}) => VoicemailMessage(
    id: id,
    callerNumber: callerNumber,
    callerName: callerName,
    durationSeconds: durationSeconds,
    listened: listened ?? this.listened,
    createdAt: createdAt,
  );

  factory VoicemailMessage.fromJson(Map<String, dynamic> json) =>
      VoicemailMessage(
        id: json['id'] as String,
        callerNumber: json['caller_number'] as String? ?? '',
        callerName: json['caller_name'] as String? ?? '',
        durationSeconds: json['duration_seconds'] as int? ?? 0,
        listened: json['listened'] as bool? ?? false,
        createdAt: DateTime.fromMillisecondsSinceEpoch(
          (json['created_at'] as int? ?? 0) * 1000,
        ),
      );

  Map<String, dynamic> toJson() => {
    'id': id,
    'caller_number': callerNumber,
    'caller_name': callerName,
    'duration_seconds': durationSeconds,
    'listened': listened,
    'created_at': createdAt.millisecondsSinceEpoch ~/ 1000,
  };
}
