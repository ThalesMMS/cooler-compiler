class Parent {
  value : Int;
  identify(x : Int) : Int { x };
};
class Child inherits Parent {
  value : Int;
  identify(x : String) : Int { 0 };
};
class Main {
  main() : Object { 0 };
};
