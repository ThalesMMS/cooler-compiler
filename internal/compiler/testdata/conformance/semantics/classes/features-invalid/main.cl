class Parent {
  inherited : Int;
  identify(x : Int) : Int { x };
};

class Child inherits Parent {
  self : Object;
  unknown : Missing;
  duplicate : Int;
  duplicate : String;
  inherited : Int;
  bad_initializer : Int <- "wrong";

  duplicate_method() : Object { 0 };
  duplicate_method(x : Int) : Object { x };
  invalid_return() : Missing { 0 };
  invalid_formals(self : SELF_TYPE, repeated : Missing, repeated : Int) : Object { 0 };
  identify() : Int { 0 };
};

class Main {
  main() : Object { 0 };
};
