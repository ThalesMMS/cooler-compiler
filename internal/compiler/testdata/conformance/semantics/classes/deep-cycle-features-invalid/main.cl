class Root {};

class A inherits B {
  self : Int;
};

class B inherits C {
  repeated() : Int { 0 };
  repeated() : Int { 1 };
};

class C inherits A {};

class D inherits C {
  bad : Missing;
};

class Main {
  main() : Object { 0 };
};
