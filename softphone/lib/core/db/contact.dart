/// A locally-stored contact: a name and a single OCN number.
class Contact {
  final String id;
  String name;
  String number; // canonical (digits-only full number, or service code)
  final int createdAt; // epoch millis

  Contact({
    required this.id,
    required this.name,
    required this.number,
    required this.createdAt,
  });

  Contact copyWith({String? name, String? number}) {
    return Contact(
      id: id,
      name: name ?? this.name,
      number: number ?? this.number,
      createdAt: createdAt,
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'number': number,
    'created_at': createdAt,
  };

  factory Contact.fromJson(Map<String, dynamic> json) => Contact(
    id: json['id'] as String,
    name: json['name'] as String,
    number: json['number'] as String,
    createdAt: json['created_at'] as int,
  );
}
