class Value {
  get() : Int { 1 };
};
class Main {
  main() : Object { let value : Value in value.get() };
};
